package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// handleNextUnit 是外部 Agent 连续写作的主入口。
//
// 一次调用回答四个问题：下一个该做哪个单元、该跑哪个阶段、能不能现在就跑、
// 以及 start_run 的确切 spec。Agent 只要「调它 → 起任务 → 轮询 → accept →
// 再调它」，不需要在提示词里背状态机。
func (s *Server) handleNextUnit(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	limit := req.GetInt("limit", 3)
	if limit <= 0 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	from := strings.TrimSpace(req.GetString("fromUnitId", ""))

	root, readOnly, err := s.deps.Store.GetBookRoot(ctx, bookID)
	if err != nil {
		return errResult(err)
	}
	snap, err := books.LoadBook(root, readOnly)
	if err != nil {
		return errResult(err)
	}

	units := sortedUnits(snap)
	started := from == ""
	out := make([]unitSnapshot, 0, limit)
	for i := range units {
		u := units[i]
		if !started {
			if u.UnitID != from {
				continue
			}
			started = true
		}
		if u.Status == domain.UnitFinalized {
			continue
		}
		out = append(out, s.buildUnitSnapshot(ctx, snap, u, previousUnitOf(snap, u.UnitID)))
		if len(out) >= limit {
			break
		}
	}

	res := map[string]any{
		"bookId":     snap.Summary.ID,
		"revision":   snap.Revision,
		"readOnly":   readOnly,
		"units":      out,
		"nextAction": nil,
	}
	if len(out) > 0 {
		res["nextAction"] = out[0]
	}
	if len(out) == 0 {
		res["note"] = "所有单元都已定稿；下一步是立项下一卷（plan mode=volume）"
	}
	return jsonResult(res)
}

// handleReviewFixables 给出能进 revise 的 issueIds。
//
// 这个判定必须由服务端给：前端 canPick、服务端 RunRevise、这里三处判据完全
// 一致，Agent 自己复刻一遍迟早会漏掉某一条（尤其是「无章号的整单元结论」），
// 而一条这样的意见就会让整批修订被拒。
func (s *Server) handleReviewFixables(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	unitID, err := req.RequireString("unitId")
	if err != nil {
		return errResult(err)
	}
	root, readOnly, err := s.deps.Store.GetBookRoot(ctx, bookID)
	if err != nil {
		return errResult(err)
	}
	snap, err := books.LoadBook(root, readOnly)
	if err != nil {
		return errResult(err)
	}
	unit, ok := findUnitIn(snap, unitID)
	if !ok {
		return errResult(fmt.Errorf("unit not found: %s", unitID))
	}
	lists, err := s.deps.Store.ListOpenCandidates(ctx, bookID)
	if err != nil {
		return errResult(err)
	}
	var reviews []domain.Candidate
	for _, c := range lists {
		if c.Kind == domain.TaskReview && candidateCoversUnit(c, unitID) {
			reviews = append(reviews, c)
		}
	}
	fixable := fixableFindings(reviews, unit.ProsePath)
	verdict, count := reviewGateFor(reviews)

	ids := make([]string, 0, len(fixable))
	for _, f := range fixable {
		ids = append(ids, f.ID)
	}
	blocking := 0
	for _, f := range fixable {
		if f.Severity == "blocking" && (f.RoundsHit == 0 || f.RoundsHit >= 2) {
			blocking++
		}
	}
	res := map[string]any{
		"unitId":         unitID,
		"reviewLists":    count,
		"verdict":        verdict,
		"fixableCount":   len(fixable),
		"blockingCount":  blocking,
		"issueIds":       ids,
		"findings":       fixable,
		"totalFindings":  countFindings(reviews),
		"skippedCount":   countFindings(reviews) - len(fixable),
		"reviseSpecHint": map[string]any{"kind": "revise", "bookId": bookID, "unitId": unitID, "issueIds": ids},
	}
	if len(fixable) == 0 {
		res["note"] = "没有可进正文补丁的意见；审校干净时下一步是 finalize"
	}
	return jsonResult(res)
}

func countFindings(cands []domain.Candidate) int {
	n := 0
	for _, c := range cands {
		n += len(c.Findings)
	}
	return n
}

// handleIdentitySegments 读/维护身份时间线。
//
// 为什么不直接让 Agent 用 write_book_file 改 YAML：那份文件有跨段约束
// （from/to 必须首尾相接、单元边界必须存在、同一 stem 不能回溯覆盖历史），
// 手写一份能解析但违反约束的文件，要等到下一次细纲接受才以一条难懂的错误爆出来。
// 这里走与界面同一条 ApplyIdentityChange / UpdateIdentitySegment / CloseIdentitySegment，
// 写之前就把不合法的组合挡掉。
func (s *Server) handleIdentitySegments(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	action := strings.ToLower(strings.TrimSpace(req.GetString("action", "list")))
	if action == "" {
		action = "list"
	}
	root, readOnly, err := s.deps.Store.GetBookRoot(ctx, bookID)
	if err != nil {
		return errResult(err)
	}
	snap, err := books.LoadBook(root, readOnly)
	if err != nil {
		return errResult(err)
	}

	if action == "list" {
		return jsonResult(map[string]any{
			"segments": identityViews(snap.IdentitySegments),
			"note":     "onStage 里出现的人物必须在目标单元有生效段（fromUnit ≤ 单元 ≤ toUnit）",
		})
	}
	if readOnly {
		return errResult(fmt.Errorf("read-only book"))
	}

	stem := strings.TrimSpace(req.GetString("stem", ""))
	if stem == "" {
		return errResult(fmt.Errorf("stem required for action=%s", action))
	}
	seg := domain.IdentitySegment{
		Stem:      stem,
		RoleTitle: strings.TrimSpace(req.GetString("roleTitle", "")),
		Org:       strings.TrimSpace(req.GetString("org", "")),
		FromUnit:  strings.TrimSpace(req.GetString("fromUnit", "")),
		Note:      strings.TrimSpace(req.GetString("note", "")),
	}
	index := req.GetInt("index", 0)

	var segments []domain.IdentitySegment
	switch action {
	case "append":
		if seg.FromUnit == "" || seg.RoleTitle == "" {
			return errResult(fmt.Errorf("append 需要 fromUnit 与 roleTitle"))
		}
		segments, err = books.ApplyIdentityChange(snap.IdentitySegments, seg, snap.Units)
	case "update":
		segments, err = books.UpdateIdentitySegment(snap.IdentitySegments, index, seg, snap.Units)
	case "close":
		segments, err = books.CloseIdentitySegment(snap.IdentitySegments, index, strings.TrimSpace(req.GetString("toUnit", "")), snap.Units)
	default:
		return errResult(fmt.Errorf("未知 action %q（list | append | update | close）", action))
	}
	if err != nil {
		return errResult(err)
	}
	if err := books.SaveIdentityTimeline(root, segments); err != nil {
		return errResult(err)
	}
	return jsonResult(map[string]any{
		"ok":       true,
		"action":   action,
		"segments": identityViews(segments),
	})
}
