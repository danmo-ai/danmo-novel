package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// unitLedgerResponse is the whole-unit ledger contract.
type unitLedgerResponse struct {
	Chapters []domain.ChapterActual `json:"chapters"`
}

// reconcileUnit asks for the entire unit's ledger in one call.
//
// 产出只是一张表，输入也就一份正文，逐章拆开纯粹是把同一份基线重复携带六遍。
// 但整份 JSON 一起承载所有章，所以任何一章不合格必须由调用方退回逐章——这里
// 只负责把不合格的原因说清楚，不替作者做「先接受一份残缺账本」的决定。
func (r *Runner) reconcileUnit(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, chapters []domain.ChapterDraft, baseline contextsvc.ReviewBaseline, modelID string) ([]domain.ChapterActual, []domain.CallRecord, error) {
	packet := contextsvc.BuildFinalizeUnitPacket(snap, unit, chapters, baseline)
	want := make([]int, 0, len(chapters))
	bodies := map[int]string{}
	for _, ch := range chapters {
		want = append(want, ch.Chapter)
		bodies[ch.Chapter] = ch.Body
	}
	var parsed unitLedgerResponse
	planned := plannedLoops(unit)
	res, err := r.runJSONWithRecord(ctx, snap, packet, modelID, domain.TaskFinalize, unit.UnitID, unit.ProsePath,
		func(raw string, cand *domain.Candidate, rec domain.CallRecord) error {
			parsed = unitLedgerResponse{}
			if err := unmarshalJSON(raw, &parsed); err != nil {
				return err
			}
			return validateUnitLedger(parsed.Chapters, want, bodies, planned)
		}, func(cand domain.Candidate) gates.Report { return gates.Report{OK: true} })
	for i := range res.Calls {
		suffix := ""
		if res.Calls[i].Step == "repair" {
			suffix = "-repair"
		}
		res.Calls[i].Step = "finalize-unit" + suffix
	}
	if err != nil {
		return nil, res.Calls, err
	}
	// 按正文顺序返回：模型输出顺序不可信，落盘与摘要投影都依赖章序。
	out := append([]domain.ChapterActual(nil), parsed.Chapters...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Chapter < out[j].Chapter })
	return out, res.Calls, nil
}

// validateUnitLedger rejects a unit ledger the accept path would reject later.
//
// 这里必须比"能解析"更严：整单元路径的失败代价是整份重跑，所以任何一处会在
// 接受时被拒的内容（漏章、摘要为空、回证对不上正文、计划伏笔没核对）都要在
// 生成阶段就判掉。判据必须与 books.ApplyFinalizationLedger 一致，否则候选能
// 生成却永远接受不了——作者只会看到一句"接受失败"，却不知道该改哪。
func validateUnitLedger(got []domain.ChapterActual, want []int, bodies map[int]string, planned map[int][]string) error {
	if len(got) == 0 {
		return fmt.Errorf("账本为空：需要每章一条")
	}
	byChapter := map[int]domain.ChapterActual{}
	for _, a := range got {
		if _, dup := byChapter[a.Chapter]; dup {
			return fmt.Errorf("第 %d 章重复出现", a.Chapter)
		}
		byChapter[a.Chapter] = a
	}
	var missing []string
	for _, ch := range want {
		a, ok := byChapter[ch]
		if !ok {
			missing = append(missing, fmt.Sprintf("第 %d 章", ch))
			continue
		}
		if strings.TrimSpace(a.Summary) == "" {
			return fmt.Errorf("第 %d 章缺少实际摘要", ch)
		}
		if err := validateChapterActual(ch, a, bodies[ch]); err != nil {
			return err
		}
		if err := validatePlannedLoops(ch, a, planned[ch]); err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("账本缺章：%s（必须每章一条）", strings.Join(missing, "、"))
	}
	for ch := range byChapter {
		if _, ok := bodies[ch]; !ok {
			return fmt.Errorf("账本出现本单元之外的章号 %d", ch)
		}
	}
	return nil
}

// validateChapterActual is the per-chapter part shared by both paths.
func validateChapterActual(chapter int, a domain.ChapterActual, body string) error {
	for _, d := range a.StateDeltas {
		if domain.CanonicalStateField(d.Field) == "" || domain.CanonicalStateField(d.Field) == "role_title" {
			return fmt.Errorf("第 %d 章实际账本字段 %s 不合法", chapter, d.Field)
		}
		if d.Confirmed && !books.ContainsVerbatim(body, d.Evidence, d.Stem) {
			return fmt.Errorf("第 %d 章状态迁移 %s.%s 没有逐字正文证据：%s", chapter, d.Stem, d.Field, d.Evidence)
		}
	}
	return nil
}

// validatePlannedLoops enforces "每章计划伏笔都要明确核对过".
//
// 接受路径会逐条要求 plannedForeshadowHandled 覆盖本章计划的埋/收条目。缺一条
// 就整份定稿接受不了，而生成阶段若不管它，作者点「接受」才被拒——那时已经没
// 有任何修复机会，只能整单元重跑。
func validatePlannedLoops(chapter int, a domain.ChapterActual, planned []string) error {
	var missing []string
	for _, id := range planned {
		if !containsString(a.PlannedForeshadowHandled, id) {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("第 %d 章计划伏笔 %s 尚未核对是否实际发生（须写进 plannedForeshadowHandled）",
			chapter, strings.Join(missing, "、"))
	}
	return nil
}

// plannedLoops lists the foreshadow ids each chapter is expected to check.
func plannedLoops(unit domain.UnitInfo) map[int][]string {
	plant, payoff := books.ChapterLoops(unit)
	out := map[int][]string{}
	for ch, ids := range plant {
		out[ch] = append(out[ch], ids...)
	}
	for ch, ids := range payoff {
		out[ch] = append(out[ch], ids...)
	}
	return out
}

// reconcileChapter produces an author-reviewable actual-ledger proposal.
// It never writes canon, prose, or continuity; only Finalize Accept commits it.
//
// 包体来自 BuildFinalizeChapterPacket：共享基线在前、本章正文与本章计划在后，
// 这样同一单元逐章调用时前缀可以复用（旧版把本章计划排在第一个 document，
// 实测命中率 6–10%）。
func (r *Runner) reconcileChapter(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, baseline contextsvc.ReviewBaseline, modelID string) (domain.ChapterActual, []domain.CallRecord, error) {
	packet := contextsvc.BuildFinalizeChapterPacket(snap, unit, ch, baseline)
	for _, b := range packet.Blocks {
		if strings.TrimSpace(b.Content) == "" && b.ID != "foreshadows" && b.ID != "continuity" {
			return domain.ChapterActual{}, nil, fmt.Errorf("定稿核对缺少 %s", b.Label)
		}
	}
	var actual domain.ChapterActual
	planned := plannedLoops(unit)[ch.Chapter]
	res, err := r.runJSONWithRecord(ctx, snap, packet, modelID, domain.TaskFinalize, unit.UnitID, unit.ProsePath,
		func(raw string, cand *domain.Candidate, rec domain.CallRecord) error {
			if err := unmarshalJSON(raw, &actual); err != nil {
				return err
			}
			if actual.Chapter != ch.Chapter || strings.TrimSpace(actual.Summary) == "" {
				return fmt.Errorf("定稿核对必须给出正确章号与摘要")
			}
			// 同一条判据也在整单元路径上：候选一旦生成就该能接受，不能等到作者
			// 点「接受」才告诉他回证对不上或计划伏笔没核对。
			if err := validateChapterActual(ch.Chapter, actual, ch.Body); err != nil {
				return err
			}
			return validatePlannedLoops(ch.Chapter, actual, planned)
		}, func(cand domain.Candidate) gates.Report { return gates.Report{OK: true} })
	// 步骤名要能看出是哪一章：六次调用的 step 曾经全是 "generate"，
	// 前端缓存表里既分不清章，也看不出哪一次是修复。
	for i := range res.Calls {
		suffix := ""
		if res.Calls[i].Step == "repair" {
			suffix = "-repair"
		}
		res.Calls[i].Step = fmt.Sprintf("finalize-ch%d%s", ch.Chapter, suffix)
	}
	return actual, res.Calls, err
}
