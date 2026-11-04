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
// 结构性缺陷（漏章/重复章/空摘要）才退回逐章；回证对不上属于 advisory，
// 由调用方挂到候选上交给作者，不再整份作废。
func (r *Runner) reconcileUnit(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, chapters []domain.ChapterDraft, baseline contextsvc.ReviewBaseline, modelID string) ([]domain.ChapterActual, []domain.ReviewFinding, []domain.CallRecord, error) {
	packet := contextsvc.BuildFinalizeUnitPacket(snap, unit, chapters, baseline)
	want := make([]int, 0, len(chapters))
	bodies := map[int]string{}
	for _, ch := range chapters {
		want = append(want, ch.Chapter)
		bodies[ch.Chapter] = ch.Body
	}
	var parsed unitLedgerResponse
	var evidenceFindings []domain.ReviewFinding
	planned := plannedLoops(unit)
	res, err := r.runJSONWithRecord(ctx, snap, packet, modelID, domain.TaskFinalize, unit.UnitID, unit.ProsePath,
		func(raw string, cand *domain.Candidate, rec domain.CallRecord) error {
			parsed = unitLedgerResponse{}
			evidenceFindings = nil
			if err := unmarshalJSON(raw, &parsed); err != nil {
				return err
			}
			fs, verr := validateUnitLedger(parsed.Chapters, want, bodies, planned)
			evidenceFindings = fs
			return verr
		}, func(cand domain.Candidate) gates.Report { return gates.Report{OK: true} })
	for i := range res.Calls {
		suffix := ""
		if res.Calls[i].Step == "repair" {
			suffix = "-repair"
		}
		res.Calls[i].Step = "finalize-unit" + suffix
	}
	if err != nil {
		return nil, nil, res.Calls, err
	}
	// 按正文顺序返回：模型输出顺序不可信，落盘与摘要投影都依赖章序。
	out := append([]domain.ChapterActual(nil), parsed.Chapters...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Chapter < out[j].Chapter })
	return out, evidenceFindings, res.Calls, nil
}

// validateUnitLedger rejects a unit ledger the accept path would reject later.
//
// 结构性缺陷（漏章、重复章、摘要为空、字段非法）必须硬拒：接受路径同样会拒，
// 生成阶段放过就等于让作者点「接受」时才失败。
//
// 但**回证对不上正文不硬拒**：请模型抄一句原文当凭据，本意是让账本可审计，
// 不是让"引号字形/代词换名"这类差异作废整份账本。实测一个单元 34 条迁移里
// 只有 2 条不过，而它们与正文逐字相同——硬拒的代价是整份重跑（一次约 1.2 万
// output token）再退回逐章，换来的是零信息量。所以这类问题记成 advisory：
// 候选卡片上标出来，作者接受前看得到，系统不替他否决。
func validateUnitLedger(got []domain.ChapterActual, want []int, bodies map[int]string, planned map[int][]string) ([]domain.ReviewFinding, error) {
	if len(got) == 0 {
		return nil, fmt.Errorf("账本为空：需要每章一条")
	}
	byChapter := map[int]domain.ChapterActual{}
	for _, a := range got {
		if _, dup := byChapter[a.Chapter]; dup {
			return nil, fmt.Errorf("第 %d 章重复出现", a.Chapter)
		}
		byChapter[a.Chapter] = a
	}
	var missing []string
	var findings []domain.ReviewFinding
	for _, ch := range want {
		a, ok := byChapter[ch]
		if !ok {
			missing = append(missing, fmt.Sprintf("第 %d 章", ch))
			continue
		}
		if strings.TrimSpace(a.Summary) == "" {
			return nil, fmt.Errorf("第 %d 章缺少实际摘要", ch)
		}
		fs, err := validateChapterActual(ch, a, bodies[ch])
		if err != nil {
			return nil, err
		}
		findings = append(findings, fs...)
		if err := validatePlannedLoops(ch, a, planned[ch]); err != nil {
			return nil, err
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("账本缺章：%s（必须每章一条）", strings.Join(missing, "、"))
	}
	for ch := range byChapter {
		if _, ok := bodies[ch]; !ok {
			return nil, fmt.Errorf("账本出现本单元之外的章号 %d", ch)
		}
	}
	return findings, nil
}

// validateChapterActual is the per-chapter part shared by both paths.
//
// 字段非法硬拒（写不进 state.yaml）；回证对不上只出 advisory——
// 见 validateUnitLedger 的说明。
func validateChapterActual(chapter int, a domain.ChapterActual, body string) ([]domain.ReviewFinding, error) {
	var findings []domain.ReviewFinding
	for _, d := range a.StateDeltas {
		if domain.CanonicalStateField(d.Field) == "" || domain.CanonicalStateField(d.Field) == "role_title" {
			return nil, fmt.Errorf("第 %d 章实际账本字段 %s 不合法", chapter, d.Field)
		}
		if d.Confirmed && !books.ContainsVerbatim(body, d.Evidence, d.Stem) {
			findings = append(findings, unverifiedEvidenceFinding(chapter, d))
		}
	}
	return findings, nil
}

// unverifiedEvidenceFinding marks one transition whose quote is not in the prose.
//
// 标题说"回证未在正文中找到"而不是"证据不合法"：这条迁移可能是对的（模型只是
// 抄错了字形或把人物名换成了代词），需要人对照正文判断。带出 from→to 与引文，
// 作者不必回原文再翻一遍才能定位。
func unverifiedEvidenceFinding(chapter int, d domain.ChapterActualDelta) domain.ReviewFinding {
	return domain.ReviewFinding{
		ID:       fmt.Sprintf("evidence-%d-%s-%s", chapter, d.Stem, d.Field),
		Severity: "advisory", Source: "local_gate", Lens: "continuity", Chapter: chapter,
		Title:      "回证未在正文中找到",
		Evidence:   fmt.Sprintf("%s.%s：%s → %s；引文「%s」", d.Stem, d.Field, d.From, d.To, clipRunes(d.Evidence, 40)),
		Suggestion: "对照本章正文核对这条迁移；若状态确实如此，直接接受即可，接受时会记入定稿日志",
	}
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
func (r *Runner) reconcileChapter(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, baseline contextsvc.ReviewBaseline, modelID string) (domain.ChapterActual, []domain.ReviewFinding, []domain.CallRecord, error) {
	packet := contextsvc.BuildFinalizeChapterPacket(snap, unit, ch, baseline)
	for _, b := range packet.Blocks {
		if strings.TrimSpace(b.Content) == "" && b.ID != "foreshadows" && b.ID != "continuity" {
			return domain.ChapterActual{}, nil, nil, fmt.Errorf("定稿核对缺少 %s", b.Label)
		}
	}
	var actual domain.ChapterActual
	var chapterFindings []domain.ReviewFinding
	planned := plannedLoops(unit)[ch.Chapter]
	res, err := r.runJSONWithRecord(ctx, snap, packet, modelID, domain.TaskFinalize, unit.UnitID, unit.ProsePath,
		func(raw string, cand *domain.Candidate, rec domain.CallRecord) error {
			chapterFindings = nil
			if err := unmarshalJSON(raw, &actual); err != nil {
				return err
			}
			if actual.Chapter != ch.Chapter || strings.TrimSpace(actual.Summary) == "" {
				return fmt.Errorf("定稿核对必须给出正确章号与摘要")
			}
			// 与整单元路径同一套判据：结构性问题硬拒（接受时同样会拒），
			// 回证对不上只出 advisory，交给作者在候选上看。
			fs, verr := validateChapterActual(ch.Chapter, actual, ch.Body)
			chapterFindings = fs
			if verr != nil {
				return verr
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
	return actual, chapterFindings, res.Calls, err
}
