package runs

import (
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// CandidateSummaryView is the list projection of a candidate: metadata plus
// findings (the review panel renders lists straight from it), with every bulk
// payload stripped. The Has* flags tell the client which detail to fetch when
// a card is expanded — chapters for prose/diff, plan maps for plan previews,
// replacement for free-form rewrites.
//
// Findings stay in the list on purpose: review lists are small and the review
// panel would otherwise need one request per list.
type CandidateSummaryView struct {
	ID            string                 `json:"id"`
	RunID         string                 `json:"runId"`
	BookID        string                 `json:"bookId"`
	Kind          domain.TaskKind        `json:"kind"`
	Status        domain.CandidateStatus `json:"status"`
	BaseRevision  string                 `json:"baseRevision"`
	TargetPath    string                 `json:"targetPath,omitempty"`
	UnitID        string                 `json:"unitId,omitempty"`
	VolumeID      string                 `json:"volumeId,omitempty"`
	Mode          string                 `json:"mode,omitempty"`
	PreservePaths []string               `json:"preservePaths,omitempty"`
	Findings      []domain.ReviewFinding `json:"findings,omitempty"`
	CreatedAt     string                 `json:"createdAt"`
	AcceptedAt    string                 `json:"acceptedAt,omitempty"`

	HasChapters     bool `json:"hasChapters"`
	HasBaseChapters bool `json:"hasBaseChapters"`
	HasPlan         bool `json:"hasPlan"`
	HasReplacement  bool `json:"hasReplacement"`
	HasOutlines     bool `json:"hasOutlines"`
	HasActual       bool `json:"hasActual"`
}

// SummarizeCandidate projects one candidate for list display.
func SummarizeCandidate(c domain.Candidate) CandidateSummaryView {
	view := CandidateSummaryView{
		ID:              c.ID,
		RunID:           c.RunID,
		BookID:          c.BookID,
		Kind:            c.Kind,
		Status:          c.Status,
		BaseRevision:    c.BaseRevision,
		TargetPath:      c.TargetPath,
		UnitID:          c.UnitID,
		VolumeID:        c.VolumeID,
		Mode:            c.Mode,
		PreservePaths:   c.PreservePaths,
		Findings:        c.Findings,
		CreatedAt:       c.CreatedAt.Format(time.RFC3339Nano),
		HasChapters:     len(c.Chapters) > 0,
		HasBaseChapters: len(c.BaseChapters) > 0,
		HasPlan:         len(c.PlanMarkdown) > 0 || len(c.PlanSettings) > 0,
		HasReplacement:  c.Replacement != "",
		HasOutlines:     len(c.UnitOutlines) > 0,
		HasActual:       len(c.ActualChapters) > 0,
	}
	if c.AcceptedAt != nil {
		view.AcceptedAt = c.AcceptedAt.Format(time.RFC3339Nano)
	}
	return view
}

// SummarizeCandidates projects a candidate list, preserving order.
func SummarizeCandidates(list []domain.Candidate) []CandidateSummaryView {
	out := make([]CandidateSummaryView, 0, len(list))
	for _, c := range list {
		out = append(out, SummarizeCandidate(c))
	}
	return out
}
