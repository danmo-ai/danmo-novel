package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/port"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

// 整单元审校引用了本单元不存在的章号时，必须当成契约违规让模型改一次，
// 而不是等作者点「自动修复」时整批报「无法定位」。
func TestWholeUnitReviewRejectsOutOfUnitChapter(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t) // 3 章
	mock := &chapterResponseLLM{}

	res, err := New(mock).RunReview(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{Concurrency: 1, Rounds: 1})
	if err == nil {
		t.Fatal("invalid chapter must surface a partial-review error")
	}
	if len(mock.calls) != 4 {
		t.Fatalf("calls=%d want chapter 1 repair plus chapters 2/3", len(mock.calls))
	}
	for _, f := range res.Candidate.Findings {
		if f.Chapter == 9 {
			t.Fatalf("out-of-unit finding leaked into the candidate: %+v", f)
		}
	}
}

// 两次都给越界章号：不能产出带野章号的候选，必须报错。
func TestWholeUnitReviewFailsWhenChapterStaysOutOfUnit(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	mock := &chapterResponseLLM{alwaysBad: true}

	res, err := New(mock).RunReview(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{Concurrency: 1, Rounds: 1})
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	for _, f := range res.Candidate.Findings {
		if f.Chapter == 9 {
			t.Fatalf("out-of-unit finding leaked into the candidate: %+v", f)
		}
	}
}

// 整单元审校同样要按契约要求章号：chapter=0 的意见无法定位，不能进候选。
type chapterResponseLLM struct {
	calls     []int
	alwaysBad bool
	noChapter bool
}

func (l *chapterResponseLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	p, _ := contextsvc.DecodeUserPayload(req.Messages[1].Content)
	ch := int(p.Extra["reviewChapter"].(float64))
	l.calls = append(l.calls, ch)
	if l.alwaysBad || ch == 1 {
		ch = 9
	}
	if l.noChapter {
		ch = 0
	}
	body, _ := json.Marshal(map[string]any{"verdict": "revise", "findings": []map[string]any{{"id": "continuity-1-1", "severity": "blocking", "chapter": ch, "lens": "continuity", "title": "时间线断裂", "evidence": "章号错误"}}})
	return port.LLMChatResponse{Content: string(body), Done: true}, nil
}

func TestWholeUnitReviewRequiresChapterNumber(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	mock := &chapterResponseLLM{noChapter: true}

	if _, err := New(mock).RunReview(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{Concurrency: 1, Rounds: 1}); err == nil {
		t.Fatal("findings without chapter must not pass whole-unit review")
	}
}
