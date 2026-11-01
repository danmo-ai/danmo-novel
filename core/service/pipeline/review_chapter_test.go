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
	// 分章预算按章数计；单章上限仍应小于整单元路径的 32768。
	if res.Packet.Policy.MaxCalls != 3 {
		t.Fatalf("maxCalls=%d want 3 (one per chapter)", res.Packet.Policy.MaxCalls)
	}
	if res.Packet.Policy.MaxOutputTokens == 0 {
		t.Fatal("policy lost: UI would show 0 budget")
	}
	// 单章预算必须明显小于整单元路径（32768），否则分章省不出余量。
	if res.Packet.Policy.MaxOutputTokens >= 32768 {
		t.Fatalf("per-chapter budget=%d should be below the unit-wide 32768", res.Packet.Policy.MaxOutputTokens)
	}
	// findings 必须带上章号，作者才能定位。
	for _, f := range res.Candidate.Findings {
		if f.Source == "ai_review" && f.Chapter == 0 {
			t.Fatalf("ai finding without chapter: %+v", f)
		}
	}
}

// ReviewCursor 让重跑跳过已完成的章，避免整单元从头再审一遍。
func TestRunReviewByChapterResumesFromCursor(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	mock := llm.NewMock().AddText(`{"verdict":"pass","findings":[]}`)
	r := New(mock)

	if _, err := r.RunReviewByChapter(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{
		SplitByChapter: true, Concurrency: 1, ReviewCursor: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if len(mock.Requests) != 1 {
		t.Fatalf("requests=%d want only the resumed chapter", len(mock.Requests))
	}
}

// 本地硬门用细纲 word_target 派生每章下限，而不是写死 2000。
func TestCheckProseUsesUnitWordTarget(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	// fixture 的 word_target 是 2500、3 章 → 每章约 833 字，正文远超下限。
	got := books.WordTargetPerChapter(unit)
	if got != 833 {
		t.Fatalf("wordTargetPerChapter=%d want 833 (2500/3)", got)
	}
	chapters := []domain.ChapterDraft{{Chapter: 1, Body: strings.Repeat("字", 1000)}}
	// 1000 > 833*0.6，不该报字数不足。
	mock := llm.NewMock().AddText(`{"verdict":"pass","findings":[]}`)
	r := New(mock)
	res, err := r.RunReview(context.Background(), snap, unit, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.LocalGate.Findings {
		if f.Title == "字数不足" {
			t.Fatalf("1000 chars should clear an 833 target: %+v", f)
		}
	}
	_ = chapters
}
