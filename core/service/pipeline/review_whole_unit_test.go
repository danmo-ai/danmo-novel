package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
)

// 整单元审校引用了本单元不存在的章号时，必须当成契约违规让模型改一次，
// 而不是等作者点「自动修复」时整批报「无法定位」。
func TestWholeUnitReviewRejectsOutOfUnitChapter(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t) // 3 章
	// 第一次故意引用第 9 章；repair 时给出合规结论。
	bad := `{"verdict":"revise","findings":[{"id":"continuity-9-1","severity":"blocking","chapter":9,"lens":"continuity","title":"时间线断裂","evidence":"第9章不存在"}]}`
	good := `{"verdict":"revise","findings":[{"id":"continuity-2-1","severity":"blocking","chapter":2,"lens":"continuity","title":"时间线断裂","evidence":"第2章前后互斥"}]}`
	mock := llm.NewMock().AddText(bad).AddText(good)

	res, err := New(mock).RunReview(context.Background(), snap, unit, "mock/mock-gpt-4")
	if err != nil {
		t.Fatalf("repair should have fixed it: %v", err)
	}
	if len(mock.Requests) != 2 {
		t.Fatalf("calls=%d want 2 (bad + repair)", len(mock.Requests))
	}
	// 重试的 payload 里要能看到"本单元的章号之一"这类可操作的说明。
	if !strings.Contains(mock.Requests[1].Messages[len(mock.Requests[1].Messages)-1].Content, "本单元的章号") {
		t.Fatal("repair payload should tell the model which chapters exist")
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
	bad := `{"verdict":"revise","findings":[{"id":"continuity-9-1","severity":"blocking","chapter":9,"lens":"continuity","title":"时间线断裂","evidence":"第9章不存在"}]}`
	mock := llm.NewMock().AddText(bad).AddText(bad)

	res, err := New(mock).RunReview(context.Background(), snap, unit, "mock/mock-gpt-4")
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
func TestWholeUnitReviewRequiresChapterNumber(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	noChapter := `{"verdict":"revise","findings":[{"id":"structure-1","severity":"advisory","title":"全单元节奏拖沓","evidence":"每章原地打转"}]}`
	mock := llm.NewMock().AddText(noChapter).AddText(noChapter)

	if _, err := New(mock).RunReview(context.Background(), snap, unit, "mock/mock-gpt-4"); err == nil {
		t.Fatal("findings without chapter must not pass whole-unit review")
	}
}
