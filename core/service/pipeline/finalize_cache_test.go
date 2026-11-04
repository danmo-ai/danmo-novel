package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/port"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// finalizeProbeLLM 记录每次定稿核对的完整 prompt，并按请求形态作答。
//
// 两条路径的契约不同（整单元 {"chapters":[…]} / 逐章 {"chapter":…}），所以应答
// 必须按请求作答，不能用队列——否则测的是调用顺序而不是校验。
type finalizeProbeLLM struct {
	// unitValid 为真时整单元请求返回合格账本；为假时返回空 chapters，
	// 让 RunFinalize 走逐章兜底（用来测逐章的缓存前缀）。
	unitValid bool

	systems []string
	users   []string
	unit    []string // 整单元请求的 prompt
	chapter []string // 逐章请求的 prompt
}

func (f *finalizeProbeLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	system := req.Messages[0].Content
	user := req.Messages[len(req.Messages)-1].Content
	prompt := system + user
	p, _ := contextsvc.DecodeUserPayload(user)
	f.systems = append(f.systems, system)
	f.users = append(f.users, user)

	if want, ok := p.Extra["finalizeChapters"].([]any); ok {
		f.unit = append(f.unit, prompt)
		if !f.unitValid {
			return port.LLMChatResponse{Content: `{"chapters":[]}`, Done: true}, nil
		}
		// 回证必须逐字出自**该章**正文：整单元包只带一份连续正文，要按章切开。
		bodies := map[int]string{}
		for _, ch := range gates.ParseProseChapters(proseDocument(user)) {
			bodies[ch.Chapter] = ch.Body
		}
		planned := map[int][]string{}
		for _, raw := range extraList(p.Extra["chapterPlans"]) {
			row, _ := raw.(map[string]any)
			ch, _ := row["chapter"].(float64)
			planned[int(ch)] = extraStrings(row["plannedForeshadow"])
		}
		chapters := make([]map[string]any, 0, len(want))
		for _, v := range want {
			ch, _ := v.(float64)
			chapters = append(chapters, ledgerRow(int(ch), bodies[int(ch)], planned[int(ch)]))
		}
		out, _ := json.Marshal(map[string]any{"chapters": chapters})
		return port.LLMChatResponse{Content: string(out), Done: true}, nil
	}

	ch := 0
	if v, ok := p.Extra["finalizeChapter"].(float64); ok {
		ch = int(v)
	}
	f.chapter = append(f.chapter, prompt)
	row := ledgerRow(ch, proseDocument(user), extraStrings(p.Extra["plannedForeshadow"]))
	out, _ := json.Marshal(row)
	return port.LLMChatResponse{Content: string(out), Done: true}, nil
}

func extraList(v any) []any {
	list, _ := v.([]any)
	return list
}

func extraStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func ledgerRow(chapter int, prose string, planned []string) map[string]any {
	// 回证必须能在正文里逐字找到：抄 payload 里已有的正文片段。
	return map[string]any{
		"chapter": chapter,
		"summary": fmt.Sprintf("第 %d 章实际发生的事", chapter),
		"stateDeltas": []map[string]any{{
			"stem": "linchen", "field": "goal", "from": "旧目标", "to": "新目标",
			"evidence": firstRuneClip(prose, 20), "confirmed": true,
		}},
		// 计划伏笔必须逐条明确回答（接受路径会逐条要求）。
		"plannedForeshadowHandled": planned,
	}
}

func proseDocument(user string) string {
	p, err := contextsvc.DecodeUserPayload(user)
	if err != nil {
		return ""
	}
	for _, d := range p.Documents {
		if d.ID == "prose" {
			return d.Content
		}
	}
	for _, d := range p.Documents {
		if strings.Contains(d.Label, "正文") {
			return d.Content
		}
	}
	return ""
}

func firstRuneClip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

// 单章单元也必须能走整单元路径：产出与逐章等价，只花一次调用。
func TestFinalizeUsesOneCallForTheWholeUnit(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ChapterFrom, unit.ChapterTo = 1, 3
	fake := &finalizeProbeLLM{unitValid: true}
	r := New(fake)

	res, err := r.RunFinalize(context.Background(), snap, unit, nil, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.unit) != 1 || len(fake.chapter) != 0 {
		t.Fatalf("want exactly one whole-unit call, got unit=%d chapter=%d", len(fake.unit), len(fake.chapter))
	}
	if len(res.Candidate.ActualChapters) != 3 {
		t.Fatalf("actual chapters=%d want 3", len(res.Candidate.ActualChapters))
	}
	for i, a := range res.Candidate.ActualChapters {
		if a.Chapter != i+1 {
			t.Fatalf("chapter order lost: %+v", res.Candidate.ActualChapters)
		}
	}
	if res.Packet.Policy.MaxCalls != 2 {
		t.Fatalf("whole-unit path must declare a 2-call budget, got %d", res.Packet.Policy.MaxCalls)
	}
}

// 整单元账本缺章 / 回证编造时必须退回逐章，而不是把残缺账本交给作者。
func TestFinalizeFallsBackToPerChapterWhenUnitLedgerIsInvalid(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ChapterFrom, unit.ChapterTo = 1, 3
	fake := &finalizeProbeLLM{unitValid: false}
	r := New(fake)

	res, err := r.RunFinalize(context.Background(), snap, unit, nil, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// 整单元 2 次（generate + repair）都返回空账本，然后逐章 3 次。
	if len(fake.unit) != 2 {
		t.Fatalf("unit attempts=%d want 2 (generate + repair)", len(fake.unit))
	}
	if len(fake.chapter) != 3 {
		t.Fatalf("chapter calls=%d want 3", len(fake.chapter))
	}
	if len(res.Candidate.ActualChapters) != 3 {
		t.Fatalf("fallback must still produce every chapter: %+v", res.Candidate.ActualChapters)
	}
	if res.Packet.Policy.MaxCalls != 2+3*2 {
		t.Fatalf("fallback budget=%d want %d", res.Packet.Policy.MaxCalls, 2+3*2)
	}
}

// 逐章路径的缓存能不能命中，取决于共享基线是否排在按章变化的内容之前。
// 旧实现把「本章计划」放成第一个 document，相邻两章从第 20 个字节就分叉——
// 实测 6 章定稿的命中率只有 6–10%，等于每章把整份上下文按全价重算。
func TestFinalizeChapterCallsShareTheBaselinePrefix(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ChapterFrom, unit.ChapterTo = 1, 3
	fake := &finalizeProbeLLM{unitValid: false}
	r := New(fake)

	if _, err := r.RunFinalize(context.Background(), snap, unit, nil, "mock/mock-gpt-4"); err != nil {
		t.Fatal(err)
	}
	if len(fake.chapter) < 3 {
		t.Fatalf("chapter calls=%d want >=3", len(fake.chapter))
	}
	// 前缀至少要盖住本章正文之前的一切：system + 共享基线 documents。
	// 用完整 prompt 自身定位，避免跨数组取 system 下标（单元尝试的调用也记在
	// systems 里，按下标对齐会错位）。
	marker := `"id":"prose"`
	for i := 1; i < len(fake.chapter); i++ {
		need := strings.Index(fake.chapter[i], marker)
		if need < 0 {
			t.Fatalf("call %d prompt has no prose block", i+1)
		}
		shared := commonPrefixLen(fake.chapter[i-1], fake.chapter[i])
		if shared < need {
			t.Fatalf("call %d shares only %d B of %d B; the baseline is re-billed per chapter",
				i+1, shared, need)
		}
	}
}

// 定稿包不能把"每章恒定"的内容漏进 documents：章号、本章计划、本章伏笔计划
// 都必须落在 extra（序列化在 documents 之后）。
func TestFinalizePacketKeepsPerChapterPartsInExtra(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	baseline := contextsvc.BuildReviewBaseline(snap, unit, nil, "")
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	if len(chapters) == 0 {
		t.Fatal("fixture has no prose")
	}
	packet := contextsvc.BuildFinalizeChapterPacket(snap, unit, chapters[0], baseline)
	p, err := contextsvc.DecodeUserPayload(packet.UserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.Documents {
		switch d.ID {
		case "chapterPlan", "chapterForeshadow", "finalizeChapter":
			t.Fatalf("per-chapter block %q leaked into documents; it must ride in extra", d.ID)
		}
	}
	if _, ok := p.Extra["chapterPlan"]; !ok {
		t.Fatal("本章计划必须随 extra 下发")
	}
	if _, ok := p.Extra["finalizeChapter"]; !ok {
		t.Fatal("章号必须随 extra 下发")
	}
}

// 整单元包同样必须把「要覆盖哪些章」的清单放 extra，documents 只留基线与正文。
func TestFinalizeUnitPacketKeepsChapterListInExtra(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	baseline := contextsvc.BuildReviewBaseline(snap, unit, nil, "")
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	packet := contextsvc.BuildFinalizeUnitPacket(snap, unit, chapters, baseline)
	p, err := contextsvc.DecodeUserPayload(packet.UserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Extra["finalizeChapters"]; !ok {
		t.Fatal("章号清单必须随 extra 下发")
	}
	for _, d := range p.Documents {
		if d.ID == "finalizeChapters" || d.ID == "chapterPlans" {
			t.Fatalf("per-unit block %q leaked into documents", d.ID)
		}
	}
	if prose := proseDocument(packet.UserPrompt); !strings.Contains(prose, "第1章") {
		t.Fatalf("whole-unit packet must carry every chapter: %q", firstRuneClip(prose, 40))
	}
}
