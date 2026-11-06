package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// 这一层解决的是「外部 Agent 怎么安全地连续写作」。
//
// 底层 5 个阶段（outline/write/review/revise/finalize）本来就都能通过 start_run
// 发起，但 Agent 要走完它们，得自己知道三件**没有写进 schema 的规则**：
//
//  1. 状态机：proposed→accepted→drafted→reviewed→finalized，每一步的输入条件
//     不同（例如 write 要求细纲已接受，review 要求正文已存在）。
//  2. 「自动修复」要提交哪些 issueIds：不是清单里的每一条都能修。整单元结论
//     （无章号的 stale-*、身份规划）和「这一章没审完」的标记送进 revise 会被
//     整批拒绝，一条就足以让另外几十条白选。
//  3. 审校干净时下一步是定稿，不是再审一遍——否则 Agent 会陷入无限重审。
//
// 这三条都是**确定性判定**，能让服务端算的就不要让 Agent 猜。下面三个工具
// 分别对应它们：next_unit / review_fixables / identity_segments。

// unitSnapshot 是给 Agent 看的一步视图：它要什么，现在卡在哪。
type unitSnapshot struct {
	UnitID     string   `json:"unitId"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	HasProse   bool     `json:"hasProse"`
	ChapterRun string   `json:"chapters"`
	NextStage  string   `json:"nextStage,omitempty"`
	NextSpec   any      `json:"nextSpec,omitempty"`
	Blocked    string   `json:"blockedBy,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

// nextStageFor 把一个单元状态映射到下一个该跑的阶段。
//
// 与 books.SelectPrimaryAction 同一套判据，但按单元逐个给：Agent 连续写作时
// 需要的是「这一个单元现在缺什么」，而不是全书一个 primary。
func nextStageFor(u domain.UnitInfo) string {
	switch u.Status {
	case domain.UnitProposed:
		return string(domain.TaskOutline)
	case domain.UnitAccepted:
		if !u.HasProse {
			return string(domain.TaskWrite)
		}
		return string(domain.TaskReview)
	case domain.UnitDrafted:
		return string(domain.TaskReview)
	case domain.UnitReviewed:
		return string(domain.TaskFinalize)
	case domain.UnitFinalized:
		return ""
	default:
		return ""
	}
}

// unitCandidates 汇总该单元名下未处理的候选，按类型分组。
func (s *Server) unitCandidates(ctx context.Context, bookID string, unitID string) map[string][]domain.Candidate {
	out := map[string][]domain.Candidate{}
	list, err := s.deps.Store.ListOpenCandidates(ctx, bookID)
	if err != nil {
		return out
	}
	for _, c := range list {
		if !candidateCoversUnit(c, unitID) {
			continue
		}
		out[string(c.Kind)] = append(out[string(c.Kind)], c)
	}
	return out
}

// candidateCoversUnit 判候选是否属于这个单元。
//
// 细纲候选是**卷级**的：`UnitID` 为空，单元身份只写在 UnitOutlines 里。
// 只比 UnitID 会把全部细纲候选漏掉——这让「有没有待处理细纲」的答案永远是
// 「没有」，Agent 于是重复发起、撞上「已有一份待处理」。
func candidateCoversUnit(c domain.Candidate, unitID string) bool {
	if c.UnitID != "" {
		return c.UnitID == unitID
	}
	for _, u := range c.UnitOutlines {
		if u.UnitID == unitID {
			return true
		}
	}
	return false
}

// buildUnitSnapshot 生成一个单元的下一步，并把「为什么卡住」写清楚。
//
// 卡住的三种真实原因都必须显式给出，否则 Agent 只能反复重试：
//   - 有同类候选待处理（服务端按单元判活跃，会直接拒绝新任务）
//   - 细纲还没接受（write 的前置条件）
//   - 上一单元还没有正文（连续写作是链式的：第一章要靠上一单元正文承接）
func (s *Server) buildUnitSnapshot(ctx context.Context, snap domain.BookSnapshot, u domain.UnitInfo, prev *domain.UnitInfo) unitSnapshot {
	out := unitSnapshot{
		UnitID:     u.UnitID,
		Title:      u.Title,
		Status:     string(u.Status),
		HasProse:   u.HasProse,
		ChapterRun: fmt.Sprintf("%d-%d", u.ChapterFrom, u.ChapterTo),
	}
	stage := nextStageFor(u)
	cands := s.unitCandidates(ctx, snap.Summary.ID, u.UnitID)

	// 审校清单干净且每章都审完时，下一步是定稿：再审一遍只会重跑一次结论
	// 不同的审校，单元永远到不了定稿。
	if stage == string(domain.TaskReview) && len(cands[string(domain.TaskReview)]) > 0 && s.deps.Runs != nil {
		if v, n := s.deps.Runs.ReviewGate(ctx, snap.Summary.ID, u.UnitID); n > 0 && v == domain.ReviewClean {
			stage = string(domain.TaskFinalize)
			out.Notes = append(out.Notes, "审校清单已无阻断且每章都审完，下一步直接定稿")
		}
	}

	out.NextStage = stage
	// 已经有审校清单时，下一步不是「再审一遍」，而是处理这份清单：
	// 有可修的问题 → revise（带 issueIds）；一条都修不了 → 归档清单后定稿。
	// 以前这里只会说 "review"，Agent 于是再起一次审校——重跑一遍结论不同的
	// 审校，清单永远关不掉。
	if lists := cands[string(domain.TaskReview)]; len(lists) > 0 &&
		(stage == string(domain.TaskReview) || stage == string(domain.TaskFinalize)) {
		fixable := fixableFindings(lists, u.ProsePath)
		verdict, _ := reviewGateFor(lists)
		out.Notes = append(out.Notes, fmt.Sprintf("已有 %d 份未处理审校清单（结论 %s）", len(lists), verdict))
		if len(fixable) > 0 {
			ids := make([]string, 0, len(fixable))
			for _, f := range fixable {
				ids = append(ids, f.ID)
			}
			out.NextStage = string(domain.TaskRevise)
			out.NextSpec = map[string]any{
				"kind":         string(domain.TaskRevise),
				"bookId":       snap.Summary.ID,
				"unitId":       u.UnitID,
				"baseRevision": snap.Revision,
				"issueIds":     ids,
			}
			out.Notes = append(out.Notes,
				fmt.Sprintf("可进正文补丁 %d 条（详见 review_fixables），其余 %d 条不参与修订",
					len(fixable), countFindings(lists)-len(fixable)))
			return out
		}
		out.Notes = append(out.Notes, "没有可进正文补丁的意见：先 accept_candidate 归档清单，再定稿")
	}

	switch stage {
	case "":
		return out
	case string(domain.TaskOutline):
		if n := len(cands[string(domain.TaskOutline)]); n > 0 {
			out.Blocked = fmt.Sprintf("已有 %d 份未接受的细纲候选：先 accept_candidate，不要重复生成", n)
			out.Notes = append(out.Notes, "候选 id："+candidateIDs(cands[string(domain.TaskOutline)]))
		}
	case string(domain.TaskWrite):
		if n := len(cands[string(domain.TaskWrite)]); n > 0 {
			out.Blocked = fmt.Sprintf("已有 %d 份未接受的正文候选：先 accept_candidate", n)
			out.Notes = append(out.Notes, "候选 id："+candidateIDs(cands[string(domain.TaskWrite)]))
		}
		if prev != nil && !prev.HasProse {
			out.Blocked = fmt.Sprintf("上一单元 %s（%s）还没有正文，本单元的第一章接不上任何上文；先补完它",
				prev.UnitID, prev.Status)
		}
	case string(domain.TaskReview):
		if n := len(cands[string(domain.TaskReview)]); n > 0 {
			out.Notes = append(out.Notes,
				fmt.Sprintf("已有 %d 份未处理审校清单：先看 review_fixables，再决定 revise 或定稿", n))
		}
	case string(domain.TaskFinalize):
		if n := len(cands[string(domain.TaskFinalize)]); n > 0 {
			out.Notes = append(out.Notes, "已有未接受的定稿候选：accept_candidate 即可，不必重跑")
		}
	}

	if stage != "" {
		out.NextSpec = map[string]any{
			"kind":         stage,
			"bookId":       snap.Summary.ID,
			"unitId":       u.UnitID,
			"baseRevision": snap.Revision,
		}
	}
	return out
}

func candidateIDs(cands []domain.Candidate) string {
	ids := make([]string, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.ID)
	}
	return strings.Join(ids, ", ")
}

// fixableFindings 返回能进 revise 的 issueId 列表。
//
// 判据与前端 canPick、服务端 RunRevise 完全一致（三处必须同一条，否则界面说
// 能修、服务端整批拒绝）：
//   - 必须有章号（chapter > 0）：整单元结论改正文修不了
//   - 不能是 review-truncated / review-incomplete：那是「这一章没审完」的标记
//   - EntityPath 若指向别的文件，也不属于正文补丁
func fixableFindings(cands []domain.Candidate, prosePath string) []domain.ReviewFinding {
	seen := map[string]domain.ReviewFinding{}
	for _, c := range cands {
		for _, f := range c.Findings {
			if !proseFixable(f, prosePath) {
				continue
			}
			if _, ok := seen[f.ID]; !ok {
				seen[f.ID] = f
			}
		}
	}
	out := make([]domain.ReviewFinding, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RoundsHit != out[j].RoundsHit {
			return out[i].RoundsHit > out[j].RoundsHit
		}
		if (out[i].Severity == "blocking") != (out[j].Severity == "blocking") {
			return out[i].Severity == "blocking"
		}
		return out[i].Chapter < out[j].Chapter
	})
	return out
}

func proseFixable(f domain.ReviewFinding, prosePath string) bool {
	if f.ID == domain.FindingReviewTruncated || f.ID == domain.FindingReviewIncomplete {
		return false
	}
	if f.Chapter <= 0 {
		return false
	}
	if p := strings.TrimSpace(f.EntityPath); p != "" && prosePath != "" && p != prosePath {
		return false
	}
	return true
}

// reviewGateFor 汇总一个单元名下全部审校清单的结论。
func reviewGateFor(cands []domain.Candidate) (string, int) {
	var all []domain.ReviewFinding
	for _, c := range cands {
		all = append(all, c.Findings...)
	}
	return domain.ReviewVerdict(all), len(cands)
}

// findUnit 按 id 取单元。
func findUnitIn(snap domain.BookSnapshot, unitID string) (domain.UnitInfo, bool) {
	for _, u := range snap.Units {
		if u.UnitID == unitID {
			return u, true
		}
	}
	return domain.UnitInfo{}, false
}

// previousUnitOf 返回顺序上的前一个单元（跨卷连续）。
func previousUnitOf(snap domain.BookSnapshot, unitID string) *domain.UnitInfo {
	sorted := append([]domain.UnitInfo(nil), snap.Units...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Volume != sorted[j].Volume {
			return sorted[i].Volume < sorted[j].Volume
		}
		return sorted[i].Index < sorted[j].Index
	})
	for i, u := range sorted {
		if u.UnitID == unitID && i > 0 {
			return &sorted[i-1]
		}
	}
	return nil
}

// sortedUnits 按卷、序返回全部单元。
func sortedUnits(snap domain.BookSnapshot) []domain.UnitInfo {
	out := append([]domain.UnitInfo(nil), snap.Units...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Volume != out[j].Volume {
			return out[i].Volume < out[j].Volume
		}
		return out[i].Index < out[j].Index
	})
	return out
}

// identityViews 把身份时间线整理成 Agent 可直接用的表。
type identityView struct {
	Stem      string `json:"stem"`
	Key       string `json:"key"`
	RoleTitle string `json:"roleTitle"`
	Org       string `json:"org,omitempty"`
	FromUnit  string `json:"fromUnit"`
	ToUnit    string `json:"toUnit,omitempty"`
	Note      string `json:"note,omitempty"`
}

func identityViews(segs []domain.IdentitySegment) []identityView {
	out := make([]identityView, 0, len(segs))
	for _, s := range segs {
		out = append(out, identityView{
			Stem: s.Stem, Key: books.SegmentKey(s), RoleTitle: s.RoleTitle,
			Org: s.Org, FromUnit: s.FromUnit, ToUnit: s.ToUnit, Note: s.Note,
		})
	}
	return out
}
