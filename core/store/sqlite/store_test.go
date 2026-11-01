package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 接受归档的审校清单只能从已终结候选里回看：open 列表必须不含它，
// history 列表必须含 accepted/rejected/expired 且按时间倒序。
func TestListCandidatesSplitsOpenAndClosed(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id string, status domain.CandidateStatus, age time.Duration) domain.Candidate {
		return domain.Candidate{
			ID: id, RunID: "run-" + id, BookID: "b1", Kind: domain.TaskReview,
			Status: status, UnitID: "v01-U1", CreatedAt: base.Add(-age),
			Findings: []domain.ReviewFinding{{ID: id + "-f", Severity: "blocking", Title: "节奏太平"}},
		}
	}
	for _, c := range []domain.Candidate{
		mk("c-open", domain.CandidateOpen, 0),
		mk("c-rejected", domain.CandidateRejected, time.Minute),
		mk("c-accepted", domain.CandidateAccepted, 2*time.Minute),
		mk("c-other-book", domain.CandidateAccepted, 3*time.Minute),
	} {
		if c.ID == "c-other-book" {
			c.BookID = "b2"
		}
		if err := st.SaveCandidate(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	open, err := st.ListOpenCandidates(ctx, "b1")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != "c-open" {
		t.Fatalf("open=%v, want only c-open", ids(open))
	}

	closed, err := st.ListClosedCandidates(ctx, "b1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(closed) != 2 || closed[0].ID != "c-rejected" || closed[1].ID != "c-accepted" {
		t.Fatalf("closed=%v, want [c-rejected c-accepted] newest first", ids(closed))
	}
	// 归档记录要能带出原 findings，否则历史区只剩空气泡。
	if len(closed[1].Findings) != 1 || closed[1].Findings[0].Title != "节奏太平" {
		t.Fatalf("accepted findings lost: %+v", closed[1].Findings)
	}
}

func ids(cs []domain.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}
