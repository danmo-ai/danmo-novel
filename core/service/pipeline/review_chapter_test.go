package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

func loadFixtureUnit(t *testing.T) (domain.BookSnapshot, domain.UnitInfo, string) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	unit := snap.Units[0]
	unit.ProseRaw = "## 第1章 夜\n\n" + strings.Repeat("他握紧刀柄，没有退缩，只把呼吸压到最慢。", 110) +
		"\n\n## 第2章 门\n\n" + strings.Repeat("石门缓缓打开，他往前迈了一步，没有回头。", 110) +
		"\n\n## 第3章 影\n\n" + strings.Repeat("影子在墙上拉长，他听见身后有人在呼吸。", 110)
	return snap, unit, root
}

// chapterAwareLLM answers each review with a response valid for the chapter in
// the request. 分章审校是并发的，队列式 mock 会因 goroutine 调度顺序不同而
// 串台——响应必须按请求作答，否则测的是调度而不是校验。
type chapterAwareLLM struct {
	mu           sync.Mutex
	calls        []string // "chapter-N" 按请求顺序
	withFindings map[int]bool
}

func (f *chapterAwareLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	payload, err := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
	if err != nil {
		return port.LLMChatResponse{}, err
	}
	chapter := 0
	if v, ok := payload.Book["chapter"]; ok {
		if n, ok := v.(float64); ok {
			chapter = int(n)
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("chapter-%d", chapter))
	wantFinding := f.withFindings[chapter]
	f.mu.Unlock()

	findings := []map[string]any{}
	if wantFinding {
		findings = append(findings, map[string]any{
			"id": fmt.Sprintf("style-%d-1", chapter), "severity": "advisory", "chapter": chapter,
			"lens": "style", "title": "节奏略平", "evidence": "全章只有一句推进",
		})
	}
	out, _ := json.Marshal(map[string]any{"verdict": "pass", "findings": findings})
	return port.LLMChatResponse{Content: string(out), Done: true}, nil
}

func (f *chapterAwareLLM) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// 分章审校必须留下真实调用记录与 policy，否则 UI 显示"调用预算 0 次"、成本不可见。
func TestRunReviewByChapterKeepsRealCallRecords(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	fake := &chapterAwareLLM{withFindings: map[int]bool{1: true}}
	r := New(fake)

	res, err := r.RunReviewByChapter(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{
		SplitByChapter: true, Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.count() != 3 {
		t.Fatalf("requests=%d want one per chapter", fake.count())
	}
	if len(res.Calls) != 3 {
		t.Fatalf("call records=%d want 3 (one per chapter)", len(res.Calls))
	}
	for _, rec := range res.Calls {
		if !strings.HasPrefix(rec.Step, "chapter-") {
			t.Fatalf("call step=%q want chapter-N", rec.Step)
		}
	}
	// 分章预算按章数计：每章最多 2 次（生成 + 一次结构修复），3 章合计上限 6。
	if res.Packet.Policy.MaxCalls != 6 {
		t.Fatalf("maxCalls=%d want 6 (two per chapter)", res.Packet.Policy.MaxCalls)
	}
	// 输出上限交给 client 按模型配置决定：上层传 0，不再自己算一个数。
	if res.Packet.Policy.MaxOutputTokens != 0 {
		t.Fatalf("maxOutputTokens=%d, want 0 (defer to model config)", res.Packet.Policy.MaxOutputTokens)
	}
	// findings 必须带上章号，作者才能定位。
	for _, f := range res.Candidate.Findings {
		if f.Source == "ai_review" && f.Chapter == 0 {
			t.Fatalf("ai finding without chapter: %+v", f)
		}
	}
}

// 截断不能被抢救路径静默收下：第一次截断必须重试一次，
// 否则一章只剩 1 条结论会被读成"这章没问题"。
func TestChapterReviewRetriesWhenProtocolSaysTruncated(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ProseRaw = "## 第1章 夜\n\n" + strings.Repeat("他握紧刀柄，没有退缩，只把呼吸压到最慢。", 110)
	fake := &truncatingReviewLLM{}
	r := New(fake)

	res, err := r.RunReviewByChapter(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{
		SplitByChapter: true, Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.count() != 2 {
		t.Fatalf("requests=%d want 2 (truncated + retry)", fake.count())
	}
	// 上层不再持有预算，重试改的是提示词（要求更紧凑），不是数字。
	for i, b := range fake.budgets {
		if b != 0 {
			t.Fatalf("call %d sent max_tokens=%d; the model config owns that value", i+1, b)
		}
	}
	for _, f := range res.Candidate.Findings {
		if f.ID == domain.FindingReviewTruncated {
			t.Fatalf("retry succeeded, truncation marker must be gone: %+v", f)
		}
	}
}

// truncatingReviewLLM: 第一次输出被模型输出上限截断（finish_reason=length +
// JSON 半截），第二次返回完整结论。截断判定只看协议字段，不看 usage。
type truncatingReviewLLM struct {
	mu      sync.Mutex
	calls   int
	budgets []int
}

func (f *truncatingReviewLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	budget := 0
	if req.GenParams != nil {
		budget = req.GenParams.MaxTokens
	}
	f.budgets = append(f.budgets, budget)
	f.mu.Unlock()

	if n == 1 {
		return port.LLMChatResponse{
			Content: `{"verdict":"pass","findings":[{"id":"style-1-1","severity":"advisory","chapter":1,"title":"半条`,
			Usage:   &port.LLMUsage{PromptTokens: 1200, CompletionTokens: 8192},
			Done:    true,
			// 协议说"撞到上限"，这才是截断的唯一证据。
			FinishReason: port.FinishReasonLength,
		}, nil
	}
	return port.LLMChatResponse{
		Content: `{"verdict":"pass","findings":[{"id":"style-1-1","severity":"advisory","chapter":1,"lens":"style","title":"节奏略平","evidence":"全章只有一句推进"}]}`,
		Usage:   &port.LLMUsage{PromptTokens: 1200, CompletionTokens: 900},
		Done:    true,
	}, nil
}

func (f *truncatingReviewLLM) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// 重审必须覆盖整个单元的每一章：以前记录过一个"从失败章继续"的游标，但那
// 个值只写回 run，新 run 永远从 0 开始——既没生效，又在候选里声称"已通过的
// 章不会重复审"。现在干脆审全单元，结论才完整。
func TestRunReviewByChapterReviewsEveryChapter(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	mock := llm.NewMock()
	for i := 0; i < 3; i++ {
		mock.AddText(`{"verdict":"pass","findings":[]}`)
	}
	r := New(mock)

	if _, err := r.RunReviewByChapter(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{
		SplitByChapter: true, Concurrency: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if len(mock.Requests) != 3 {
		t.Fatalf("requests=%d want one per chapter (3)", len(mock.Requests))
	}
}

// 每章目标只由 books.ChapterTarget 决定：细纲写了 chapter_chars 就用它，
// 没有就按 word_target÷章数，且结果一律夹进合法区间。
func TestChapterTargetIsTheOnlyWordcountSource(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	// fixture 的 chapter_chars 是 2500（3 章），与 word_target 7500 自洽。
	if got := books.ChapterTarget(unit); got != 2500 {
		t.Fatalf("chapterTarget=%d want 2500", got)
	}
	// 老书只有单元总字数：30000 / 6 章 = 5000，必须夹到单章上限，而不是照单全收。
	legacy := domain.UnitInfo{
		ChapterFrom: 1, ChapterTo: 6,
		OutlineRaw: "word_target: 30000\n",
	}
	if got := books.ChapterTarget(legacy); got != domain.ChapterCharsMax {
		t.Fatalf("chapterTarget=%d want %d (5000 must clamp)", got, domain.ChapterCharsMax)
	}
	// 低于下限同样夹回来：2500 总字数摊到 3 章是 833。
	tiny := domain.UnitInfo{ChapterFrom: 1, ChapterTo: 3, OutlineRaw: "word_target: 2500\n"}
	if got := books.ChapterTarget(tiny); got != domain.ChapterCharsMin {
		t.Fatalf("chapterTarget=%d want %d", got, domain.ChapterCharsMin)
	}
	// 两个键都没有 → 默认值。
	if got := books.ChapterTarget(domain.UnitInfo{ChapterFrom: 1, ChapterTo: 2}); got != domain.ChapterCharsDefault {
		t.Fatalf("chapterTarget=%d want default %d", got, domain.ChapterCharsDefault)
	}

	// 1000 字以下不到 2500×0.6=1500，该报字数不足。
	unit.ProseRaw = "## 第1章 夜\n\n" + strings.Repeat("字", 1000)
	mock := llm.NewMock().AddText(`{"verdict":"pass","findings":[]}`)
	r := New(mock)
	res, err := r.RunReview(context.Background(), snap, unit, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	var short bool
	for _, f := range res.LocalGate.Findings {
		if f.Title == "字数不足" {
			short = true
		}
	}
	if !short {
		t.Fatalf("1000 chars must be short of a 2500 target: %+v", res.LocalGate.Findings)
	}
}

// 超出目标上限也要报（以前只判下限，写多长都算通过）。
func TestProseOverTargetIsReported(t *testing.T) {
	body := strings.Repeat("字", 4000) // 2500 × 1.4 = 3500 以上
	rep := gates.CheckProseWithRules([]domain.ChapterDraft{{Chapter: 1, Body: body}}, 2500, nil)
	var over bool
	for _, f := range rep.Findings {
		if f.Title == "字数超出目标区间" {
			over = true
		}
	}
	if !over {
		t.Fatalf("4000 chars against a 2500 target should be flagged: %+v", rep.Findings)
	}
}
