package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestFinalizeProducesChapterActualCandidateWithoutWriting(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 1, OutlinePath: "outline/units/v01-U1.yaml", ProsePath: "units/v01-U1.md", ProseRaw: "## 第1章 留门\n\n林尘仍在外门，没有进山。", OutlineRaw: "unit_id: v01-U1\non_stage: [lin]\npov: lin\nstory_day: 1\nend_day: 1\nchapters:\n  - chapter: 1\n    goal: 留在外门\n    change: 无迁移\n    cut_hook: 门未开\n"}
	mock := llm.NewMock().AddText(`{"chapter":1,"summary":"林尘仍在外门，门未开","stateDeltas":[],"plant":[],"payoff":[],"plannedForeshadowHandled":[]}`)
	res, err := New(mock).RunFinalize(context.Background(), domain.BookSnapshot{Summary: domain.BookSummary{ID: "b"}}, unit, nil, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidate.ActualChapters) != 1 || res.Candidate.ActualChapters[0].Summary == "" {
		t.Fatalf("missing actual ledger candidate: %+v", res.Candidate)
	}
	if len(mock.Requests) != 1 || !strings.Contains(mock.Requests[0].Messages[1].Content, "没有进山") {
		t.Fatal("reconcile must receive actual chapter prose")
	}
	if res.Candidate.Status != domain.CandidateOpen || res.Packet.Policy.MaxCalls != 2 {
		t.Fatalf("finalize candidate/policy invalid: %+v", res)
	}
}
