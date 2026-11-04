package pipeline

import (
	"context"
	"github.com/danmo-ai/danmo-novel/core/domain"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// RunOutlineReview performs one bounded semantic review of an accepted outline.
// It cannot repair or write the outline; only a new outline candidate may do so.
func (r *Runner) RunOutlineReview(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, modelID string) (Result, error) {
	packet := contextsvc.BuildOutlineReviewPacket(snap, unit)
	res, err := r.runJSON(ctx, snap, packet, modelID, domain.TaskOutlineReview, unit.UnitID, unit.OutlinePath, func(raw string, cand *domain.Candidate) error {
		findings, err := parseReviewResponse(raw, 0)
		if err != nil {
			return err
		}
		for i := range findings {
			if findings[i].Chapter == 0 {
				findings[i].Chapter = unit.ChapterFrom
			}
			findings[i].UnitID = unit.UnitID
		}
		cand.Findings = findings
		return nil
	}, func(cand domain.Candidate) gates.Report { return gates.Report{OK: true, Counts: map[string]int{}} })
	return res, err
}
