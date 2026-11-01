package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// 修订只改有 findings 的章：未涉及的章必须逐字保留，否则"最小改动"无法兑现。
func TestRunReviseOnlyTouchesTargetedChapters(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	mock := llm.NewMock().
		AddText(`{"chapter":2,"title":"门·修","body":"第2章修订后的正文"}`)
	r := New(mock)

	findings := []domain.ReviewFinding{
		{ID: "f2", Severity: "blocking", Chapter: 2, Title: "位置跳变"},
	}
	res, err := r.RunRevise(context.Background(), snap, unit, findings, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// 修订前的正文必须留在候选里，否则前端无法 diff，接受就是盲签。
	if len(res.Candidate.BaseChapters) != len(res.Candidate.Chapters) {
		t.Fatalf("baseChapters=%d chapters=%d, want same length for diff",
			len(res.Candidate.BaseChapters), len(res.Candidate.Chapters))
	}
	origChapters := gates.ParseProseChapters(unit.ProseRaw)
	for i, ch := range res.Candidate.BaseChapters {
		if ch.Body != origChapters[i].Body {
			t.Fatalf("base chapter %d is not the pre-revise prose", ch.Chapter)
		}
	}
	// 只有第 2 章需要改 → 只发一次调用。
	if len(mock.Requests) != 1 {
		t.Fatalf("requests=%d want 1 (only chapter 2)", len(mock.Requests))
	}
	if len(res.Candidate.Chapters) != 3 {
		t.Fatalf("chapters=%d want all 3 kept", len(res.Candidate.Chapters))
	}
	if !strings.Contains(res.Candidate.Chapters[1].Body, "修订后") {
		t.Fatalf("chapter 2 not revised: %q", res.Candidate.Chapters[1].Body)
	}
	// 逐字比对：第 1、3 章必须与原文完全一致。
	for _, i := range []int{0, 2} {
		if res.Candidate.Chapters[i].Body != origChapters[i].Body {
			t.Fatalf("chapter %d modified although out of scope", i+1)
		}
	}
}

// 模型返回错误章号时必须拒绝，不能把别的章的正文写回目标章。
func TestRunReviseRejectsWrongChapter(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	mock := llm.NewMock().
		AddText(`{"chapter":9,"title":"错章","body":"不该被接受"}`)
	r := New(mock)

	_, err := r.RunRevise(context.Background(), snap, unit,
		[]domain.ReviewFinding{{ID: "f2", Severity: "blocking", Chapter: 2, Title: "x"}},
		"mock/mock-gpt-4")
	if err == nil {
		t.Fatal("wrong chapter number must be rejected")
	}
	if !strings.Contains(err.Error(), "chapter") {
		t.Fatalf("error should mention chapter: %v", err)
	}
}

func splitFixtureChapters(t *testing.T, raw string) []domain.ChapterDraft {
	t.Helper()
	return gates.ParseProseChapters(raw)
}

var _ = splitFixtureChapters
