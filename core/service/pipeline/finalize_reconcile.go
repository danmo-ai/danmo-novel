package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/resource/novel"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// reconcileChapter produces an author-reviewable actual-ledger proposal.
// It never writes canon, prose, or continuity; only Finalize Accept commits it.
func (r *Runner) reconcileChapter(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, modelID string) (domain.ChapterActual, []domain.CallRecord, error) {
	blocks := []domain.ContextBlock{
		{ID: "contract", Label: "本章计划", Source: unit.OutlinePath, Content: contextsvc.ChapterContract(unit.OutlineRaw, ch.Chapter)},
		{ID: "prose", Label: "本章实际正文", Source: unit.ProsePath, Content: ch.Body},
		{ID: "foreshadows", Label: "本章伏笔计划", Source: "canon/foreshadow-schedule.md", Content: contextsvc.ForeshadowForChapter(snap, unit, ch.Chapter)},
		{ID: "continuity", Label: "入场连续状态", Source: "continuity/state.yaml", Content: contextsvc.ExtractContinuity(snap, unit, books.OnStageFromOutline(unit.OutlineRaw), contextsvc.ContinuityForReview)},
	}
	packet := domain.ContextPacket{BookID: snap.Summary.ID, UnitID: unit.UnitID, TaskKind: domain.TaskFinalize, Policy: domain.CallPolicy{MaxCalls: 2, AllowOneRepair: true, TimeoutSec: 600}, SystemPrompt: novel.FinalizeReconcileSystemPrompt()}
	for i := range blocks {
		if strings.TrimSpace(blocks[i].Content) == "" && blocks[i].ID != "foreshadows" && blocks[i].ID != "continuity" {
			return domain.ChapterActual{}, nil, fmt.Errorf("定稿核对缺少 %s", blocks[i].Label)
		}
	}
	packet.UserPrompt = contextsvc.BuildUserPrompt("finalize_reconcile", "", map[string]any{"id": snap.Summary.ID, "unitId": unit.UnitID}, "只核对第 "+fmt.Sprint(ch.Chapter)+" 章实际正文。不得把计划自动视为已发生。", blocks, map[string]any{"chapter": ch.Chapter})
	var actual domain.ChapterActual
	res, err := r.runJSON(ctx, snap, packet, modelID, domain.TaskFinalize, unit.UnitID, unit.ProsePath, func(raw string, cand *domain.Candidate) error {
		if err := unmarshalJSON(raw, &actual); err != nil {
			return err
		}
		if actual.Chapter != ch.Chapter || strings.TrimSpace(actual.Summary) == "" {
			return fmt.Errorf("定稿核对必须给出正确章号与摘要")
		}
		return nil
	}, func(cand domain.Candidate) gates.Report { return gates.Report{OK: true} })
	return actual, res.Calls, err
}
