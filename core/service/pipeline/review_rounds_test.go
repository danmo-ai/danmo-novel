package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// roundScriptLLM 按调用顺序返回固定响应，并记录每次的 system prompt 与 payload，
// 用来验证「多轮共用同一前缀」和「并集去重」这两件事。
type roundScriptLLM struct {
	mu        sync.Mutex
	cursor    int
	responses []string
	systems   []string
	payloads  []*contextsvc.UserPayload
}

func (p *roundScriptLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(req.Messages) > 0 {
		p.systems = append(p.systems, req.Messages[0].Content)
	}
	payload, err := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
	if err == nil {
		p.payloads = append(p.payloads, &payload)
	}
	if p.cursor >= len(p.responses) {
		return port.LLMChatResponse{Content: `{"verdict":"pass","findings":[]}`, Done: true}, nil
	}
	out := p.responses[p.cursor]
	p.cursor++
	return port.LLMChatResponse{Content: out, Done: true}, nil
}

func (p *roundScriptLLM) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.systems)
}

// 同一处缺陷，三轮措辞完全不同但 anchor 一致 → 必须合并成一条，且 roundsHit=3。
//
// 这是多轮方案的落地验收：实测单轮只覆盖约 31% 的问题，靠并集提覆盖率；
// 但三轮对同一处会给出毫不相干的措辞（"车斗空着却在卸货" vs "拉货进度自相矛盾"），
// 只有 anchor 区间能把它们认成同一处。
func TestReviewChapterRunsAllRoundsAndDeduplicates(t *testing.T) {
	snap, unit := reviseFixture(t)
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	ch := chapters[1] // 第 2 章：anchor 在这段正文里唯一
	body := ch.Body

	anchor := "石门缓缓打开，他往前迈了一步，没有回头。"
	one := `{"verdict":"revise","findings":[{"id":"c-1","severity":"advisory","chapter":2,` +
		`"title":"车斗空着却在卸货","evidence":"石门开着却没人","anchor":"` + anchor + `"}]}`
	two := `{"verdict":"revise","findings":[{"id":"c-2","severity":"blocking","chapter":2,` +
		`"title":"进度自相矛盾","evidence":"门开的方向不对","anchor":"` + anchor + `"},` +
		`{"id":"s-1","severity":"advisory","chapter":2,"title":"停顿模板","evidence":"停顿"}]}`
	three := `{"verdict":"revise","findings":[{"id":"c-3","severity":"advisory","chapter":2,` +
		`"title":"门开得不对","evidence":"门","anchor":"` + anchor + `"}]}`

	fake := &roundScriptLLM{responses: []string{one, two, three}}
	r := New(fake)

	baseline := contextsvc.BuildReviewBaseline(snap, unit, nil, "")
	packet := contextsvc.BuildChapterReviewPacket(snap, unit, ch, "", baseline)
	got, calls, err := r.reviewChapter(context.Background(), "mock/mock-gpt-4", packet, ch.Chapter, 3, body)
	if err != nil {
		t.Fatal(err)
	}

	// 1. 三轮都跑了。
	if fake.count() != 3 {
		t.Fatalf("calls=%d want 3 (one per round)", fake.count())
	}
	if len(calls) != 3 {
		t.Fatalf("call records=%d want 3", len(calls))
	}
	// 2. 调用记录按轮次分标，成本才可归因。
	for i, c := range calls {
		want := "chapter-" + itoa(ch.Chapter) + "-r" + itoa(i+1)
		if c.Step != want {
			t.Fatalf("call %d step=%q want %q", i, c.Step, want)
		}
	}
	// 3. 三轮共用同一个 system prompt（前缀缓存的前提）。
	for i := 1; i < len(fake.systems); i++ {
		if fake.systems[i] != fake.systems[0] {
			t.Fatalf("round %d changed the system prompt; prefix cache would miss", i+1)
		}
	}
	// 4. 并集去重：同锚点的三条合成一条（severity 取最严），另一条独立保留。
	if len(got) != 2 {
		t.Fatalf("union size=%d want 2: %+v", len(got), titlesOf(got))
	}
	var merged *domain.ReviewFinding
	for i := range got {
		if strings.Contains(got[i].Title, "门") || strings.Contains(got[i].Title, "斗") || strings.Contains(got[i].Title, "进度") {
			merged = &got[i]
		}
	}
	if merged == nil {
		t.Fatalf("merged finding missing: %v", titlesOf(got))
	}
	if merged.RoundsHit != 3 {
		t.Fatalf("roundsHit=%d want 3", merged.RoundsHit)
	}
	if merged.Severity != "blocking" {
		t.Fatalf("severity=%q want blocking (strictest of three rounds)", merged.Severity)
	}
	// 5. 排序把复现次数多的排前面：3 轮并集条数翻倍，不排序作者无从判断。
	if got[0].RoundsHit != 3 {
		t.Fatalf("first finding roundsHit=%d want the consensus one first", got[0].RoundsHit)
	}
}

// 轮数必须真的乘上调用次数，并且 Instruction 里的轮次标记确实变了。
func TestReviewChapterRoundInstructionChanges(t *testing.T) {
	snap, unit := reviseFixture(t)
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	ch := chapters[0]
	fake := &roundScriptLLM{}
	r := New(fake)
	baseline := contextsvc.BuildReviewBaseline(snap, unit, nil, "")
	packet := contextsvc.BuildChapterReviewPacket(snap, unit, ch, "", baseline)

	if _, _, err := r.reviewChapter(context.Background(), "mock/mock-gpt-4", packet, ch.Chapter, 2, ch.Body); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 2 {
		t.Fatalf("calls=%d want 2", fake.count())
	}
	if len(fake.payloads) < 2 {
		t.Fatal("payloads not captured")
	}
	first, second := fake.payloads[0].Instruction, fake.payloads[1].Instruction
	if !strings.Contains(first, "第 1/2 轮") {
		t.Fatalf("first round instruction missing marker: %q", first)
	}
	if !strings.Contains(second, "第 2/2 轮") {
		t.Fatalf("second round instruction missing marker: %q", second)
	}
	// 任务标签不变：轮次只该落在 instruction 尾部，否则前缀就散了。
	if fake.payloads[0].Task != fake.payloads[1].Task {
		t.Fatal("round marker must not change the task tag")
	}
	// 结构化数据（正文所在部分）必须逐字相同。
	b0, _ := json.Marshal(fake.payloads[0].Documents)
	b1, _ := json.Marshal(fake.payloads[1].Documents)
	if string(b0) != string(b1) {
		t.Fatal("documents changed between rounds; prefix cache would miss")
	}
}

func titlesOf(fs []domain.ReviewFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Title)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
