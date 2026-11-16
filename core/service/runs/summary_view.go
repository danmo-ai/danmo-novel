package runs

import (
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// SummaryView is the list projection of a run: everything the logs table and
// the workspace's run strip render, without the per-call records. The totals
// are precomputed here because stripping Calls would otherwise leave the list
// unable to show token/cache/duration sums.
//
// It is a separate type (not domain.Run with nil Calls) so the aggregate
// fields only ever appear on the summary endpoint; the full run contract and
// its stored payload stay untouched.
type SummaryView struct {
	ID          string            `json:"id"`
	BookID      string            `json:"bookId"`
	Kind        domain.TaskKind   `json:"kind"`
	Status      domain.RunStatus  `json:"status"`
	Spec        domain.TaskSpec   `json:"spec"`
	Policy      domain.CallPolicy `json:"policy"`
	ModelID     string            `json:"modelId"`
	CallsUsed   int               `json:"callsUsed"`
	CandidateID string            `json:"candidateId,omitempty"`
	Error       string            `json:"error,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`

	InputTokens   int   `json:"inputTokens"`
	OutputTokens  int   `json:"outputTokens"`
	DurationMs    int64 `json:"durationMs"`
	CacheRead     int   `json:"cacheRead"`
	CacheCreation int   `json:"cacheCreation"`
}

// Summarize projects one run for list display.
func Summarize(run domain.Run) SummaryView {
	view := SummaryView{
		ID:          run.ID,
		BookID:      run.BookID,
		Kind:        run.Kind,
		Status:      run.Status,
		Spec:        run.Spec,
		Policy:      run.Policy,
		ModelID:     run.ModelID,
		CallsUsed:   run.CallsUsed,
		CandidateID: run.CandidateID,
		Error:       run.Error,
		Warnings:    run.Warnings,
		CreatedAt:   run.CreatedAt,
		UpdatedAt:   run.UpdatedAt,
	}
	for _, call := range run.Calls {
		view.InputTokens += call.PromptTokens
		view.OutputTokens += call.CompletionTokens
		view.DurationMs += call.DurationMs
		view.CacheRead += call.CacheReadTokens
		view.CacheCreation += call.CacheCreationTokens
	}
	return view
}

// SummarizeAll projects a run list, preserving order.
func SummarizeAll(runs []domain.Run) []SummaryView {
	out := make([]SummaryView, 0, len(runs))
	for _, run := range runs {
		out = append(out, Summarize(run))
	}
	return out
}
