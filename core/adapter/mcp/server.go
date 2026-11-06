// Package mcp exposes Danmo Novel capabilities over Model Context Protocol
// (streamable-http). Tools are thin wrappers around the same services that
// back the REST API — generation stays inside the controlled pipeline (LLM
// calls still forbid tools); MCP is only an orchestration surface for hosts
// such as Danmo Work experts.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/runs"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
	"github.com/danmo-ai/danmo-novel/core/version"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Deps are the services the MCP tools operate on. The package takes a
// dependency-injection shape (not *httpapi.App) so the HTTP adapter can mount
// it without an import cycle.
type Deps struct {
	Settings *config.Manager
	Store    *sqlite.Store
	Runs     *runs.Service
}

// Server wraps the MCP server with Danmo Novel tool handlers.
type Server struct {
	deps *Deps
	mcp  *mcpserver.MCPServer
	http http.Handler
}

// New builds the MCP server and its streamable-http handler.
func New(deps *Deps) *Server {
	s := &Server{deps: deps}
	s.mcp = mcpserver.NewMCPServer(
		"danmo-novel",
		version.Version,
		mcpserver.WithToolCapabilities(false),
	)
	s.registerTools()
	s.http = mcpserver.NewStreamableHTTPServer(
		s.mcp,
		mcpserver.WithEndpointPath("/mcp"),
		mcpserver.WithStateLess(true),
	)
	return s
}

// Handler returns the streamable-http transport handler (POST/GET/DELETE).
func (s *Server) Handler() http.Handler { return s.http }

func jsonResult(v any) (*mcpgo.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpgo.NewToolResultErrorFromErr("marshal result", err), nil
	}
	return mcpgo.NewToolResultText(string(b)), nil
}

func errResult(err error) (*mcpgo.CallToolResult, error) {
	return mcpgo.NewToolResultErrorFromErr("", err), nil
}

func (s *Server) registerTools() {
	// 只保留 Agent 连续写作必需的工具。调试/管理类（health、list_books、
	// create_book、import_book、accept_volume、promote_cast、preview_run、
	// list_runs、list_candidates、get_candidate、cancel_run）已移除：它们
	// 与「按单元推进」无关，只会让 Agent 的选择变复杂。
	s.mcp.AddTool(mcpgo.NewTool("next_unit",
		mcpgo.WithDescription("The one tool an agent needs to drive continuous writing. Returns the earliest unfinished unit "+
			"with its nextStage (outline/write/review/revise/finalize), a ready-to-use nextSpec for start_run, and blockedBy "+
			"when it must not start (pending candidate, unaccepted outline, or previous unit still has no prose). "+
			"Call it after every accept: it always reflects the state on disk."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithString("fromUnitId", mcpgo.Description("Optional: start scanning at this unit instead of the first unfinished one")),
		mcpgo.WithNumber("limit", mcpgo.Description("How many units to include (default 3)")),
	), s.handleNextUnit)

	s.mcp.AddTool(mcpgo.NewTool("start_run",
		mcpgo.WithDescription("Start a controlled generation run for one unit (same service call as REST POST /api/runs). "+
			"Pass bookId + unitId + kind + baseRevision (next_unit's nextSpec already carries it). "+
			"review auto-sets supersedeReview. revise defaults to every patchable finding; pass issueIds to fix only a subset. "+
			"Poll with get_run until status leaves running/pending. No tools are sent to the LLM."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithString("unitId", mcpgo.Required(), mcpgo.Description("Unit id, e.g. v02-U2")),
		mcpgo.WithString("kind", mcpgo.Required(), mcpgo.Description("outline | write | review | revise | finalize")),
		mcpgo.WithString("baseRevision", mcpgo.Description("Revision the caller read; next_unit returns it")),
		mcpgo.WithString("instruction", mcpgo.Description("Optional author instruction appended to the task")),
		mcpgo.WithArray("issueIds", mcpgo.Description("revise only: fix just these findings (must be patchable; see review_fixables)")),
	), s.handleStartRun)

	s.mcp.AddTool(mcpgo.NewTool("get_run",
		mcpgo.WithDescription("Get a run by id (poll until status leaves running/pending)."),
		mcpgo.WithString("runId", mcpgo.Required()),
	), s.handleGetRun)

	s.mcp.AddTool(mcpgo.NewTool("accept_candidate",
		mcpgo.WithDescription("Accept a candidate: the ONLY path that writes canon/prose and advances status."),
		mcpgo.WithObject("command", mcpgo.Required(), mcpgo.Description("AcceptCommand JSON (candidateId, baseRevision, forceRiskAccept)")),
	), s.handleAcceptCandidate)

	s.mcp.AddTool(mcpgo.NewTool("reject_candidate",
		mcpgo.WithDescription("Reject a candidate with a reason (drops the candidate, does not change prose)."),
		mcpgo.WithString("candidateId", mcpgo.Required()),
		mcpgo.WithString("reason", mcpgo.Description("Reason")),
	), s.handleRejectCandidate)

	s.mcp.AddTool(mcpgo.NewTool("review_fixables",
		mcpgo.WithDescription("List the finding ids of a unit's open review lists that can actually go into revise. "+
			"Excludes unit-level findings (no chapter) and review-truncated/review-incomplete markers. "+
			"Pass the returned issueIds to start_run{kind:revise}."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithString("unitId", mcpgo.Required()),
	), s.handleReviewFixables)

	s.mcp.AddTool(mcpgo.NewTool("read_book_file",
		mcpgo.WithDescription("Read a single file from a book tree (relative path)."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithString("path", mcpgo.Required()),
	), s.handleReadBookFile)

	s.mcp.AddTool(mcpgo.NewTool("write_book_file",
		mcpgo.WithDescription("Write a single file in a book tree (author fill-in only; prose/canon still go through candidates + accept)."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithString("path", mcpgo.Required()),
		mcpgo.WithString("content", mcpgo.Required()),
	), s.handleWriteBookFile)

	s.mcp.AddTool(mcpgo.NewTool("identity_segments",
		mcpgo.WithDescription("Read canon/identity-timeline.yaml (action=list). "+
			"Use edit_field(kind=identity) to modify it."),
		mcpgo.WithString("bookId", mcpgo.Required()),
	), s.handleIdentitySegments)

	s.mcp.AddTool(mcpgo.NewTool("get_book_template",
		mcpgo.WithDescription("Read the book structure template (docs/novel-book-templates.md). "+
			"Contains field definitions and a repair guide: which file/field to edit when an error occurs."),
	), s.handleGetBookTemplate)
}

// handleGetBookTemplate 返回书籍结构模板（含修复指南）。
//
// 这是 Agent「遇到错误时怎么修」的知识来源：模板里写了字段定义、错误归口、
// 修复语法。Agent 不需要背下全部模板，只要能把错误文本对上表就能自己修。
func (s *Server) handleGetBookTemplate(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	// 模板是静态文档，从仓库里读。
	path := "docs/novel-book-templates.md"
	raw, err := os.ReadFile(path)
	if err != nil {
		return errResult(err)
	}
	return jsonResult(map[string]any{"path": path, "content": string(raw)})
}

// ---- handlers ----

func (s *Server) handleReadBookFile(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	rel, err := req.RequireString("path")
	if err != nil {
		return errResult(err)
	}
	content, err := readBookFile(ctx, s.deps.Store, bookID, rel)
	if err != nil {
		return errResult(err)
	}
	return jsonResult(map[string]any{"path": rel, "content": content})
}

func (s *Server) handleWriteBookFile(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	rel, err := req.RequireString("path")
	if err != nil {
		return errResult(err)
	}
	content, err := req.RequireString("content")
	if err != nil {
		return errResult(err)
	}
	if err := writeBookFile(ctx, s.deps.Store, bookID, rel, content); err != nil {
		return errResult(err)
	}
	return jsonResult(map[string]any{"ok": true, "path": rel})
}

// handleStartRun 启动一个单元生产任务。
//
// 与 REST 的 POST /api/runs 走同一个服务层（Runs.Start），参数语义也一致；
// MCP 只多做了两件「Agent 容易做错、且错了必死」的事，都没有另起链路：
//
//  1. review 自动置 supersedeReview。重审的语义是用新结论替换旧结论；不归档
//     旧清单的话，那条 waiting_accept 的 run 会一直占着单元，重审永远起不来。
//  2. revise 的 issueIds 默认取「可进正文补丁的全部条目」。这条判据在
//     review_fixables 里已实现，与前端 canPick、服务端 RunRevise 同源；
//     Agent 若显式传 issueIds，则以它为准（只校验、不覆盖），这样仍能只修
//     其中一部分。
//
// baseRevision 仍由调用方传（MCP 与 REST 同语义）：它表达的是「我看到的是哪一版」。
// next_unit 的 nextSpec 里已经带好，原样回传即可。
func (s *Server) handleStartRun(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	unitID, err := req.RequireString("unitId")
	if err != nil {
		return errResult(err)
	}
	kindText, err := req.RequireString("kind")
	if err != nil {
		return errResult(err)
	}
	kind := domain.TaskKind(kindText)
	switch kind {
	case domain.TaskOutline, domain.TaskWrite, domain.TaskReview, domain.TaskRevise, domain.TaskFinalize:
	default:
		return errResult(fmt.Errorf("未知 kind %q（outline | write | review | revise | finalize）", kindText))
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

	spec := domain.TaskSpec{
		Kind: kind, BookID: bookID, UnitID: unitID,
		BaseRevision: strings.TrimSpace(req.GetString("baseRevision", "")),
		Instruction:  strings.TrimSpace(req.GetString("instruction", "")),
	}
	if kind == domain.TaskReview {
		spec.SupersedeReview = true
	}
	if kind == domain.TaskRevise {
		lists, _ := s.deps.Store.ListOpenCandidates(ctx, bookID)
		var reviews []domain.Candidate
		for _, c := range lists {
			if c.Kind == domain.TaskReview && candidateCoversUnit(c, unitID) {
				reviews = append(reviews, c)
			}
		}
		fixable := fixableFindings(reviews, unit.ProsePath)
		if len(fixable) == 0 {
			return errResult(fmt.Errorf("单元 %s 没有可进正文补丁的审校意见；审校干净时下一步是 finalize", unitID))
		}
		allowed := make(map[string]bool, len(fixable))
		for _, f := range fixable {
			allowed[f.ID] = true
		}
		requested := req.GetStringSlice("issueIds", nil)
		if len(requested) == 0 {
			// 默认修全部：与脚本「全选 findings」的行为一致。
			for _, f := range fixable {
				spec.IssueIDs = append(spec.IssueIDs, f.ID)
			}
		} else {
			// Agent 显式挑选：只接受可进补丁的条目。混进一条整单元结论会让
			// 服务端整批拒绝，所以这里先逐条挡下并说清是哪几条。
			var rejected []string
			for _, id := range requested {
				if allowed[id] {
					spec.IssueIDs = append(spec.IssueIDs, id)
				} else {
					rejected = append(rejected, id)
				}
			}
			if len(rejected) > 0 {
				return errResult(fmt.Errorf(
					"这些 issueId 不能进正文补丁（整单元结论，或「这一章没审完」的标记）：%s；"+
						"可用的见 review_fixables", strings.Join(rejected, ", ")))
			}
		}
	}

	run, err := s.deps.Runs.Start(ctx, spec)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error(), "run": run})
	}
	return jsonResult(run)
}

func (s *Server) handleGetRun(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	runID, err := req.RequireString("runId")
	if err != nil {
		return errResult(err)
	}
	run, err := s.deps.Store.GetRun(ctx, runID)
	if err != nil {
		return errResult(err)
	}
	return jsonResult(run)
}

func (s *Server) handleAcceptCandidate(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	var cmd domain.AcceptCommand
	raw, _ := json.Marshal(req.GetArguments()["command"])
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return errResult(fmt.Errorf("decode command: %w", err))
	}
	if cid, ok := req.GetArguments()["candidateId"].(string); ok && cmd.CandidateID == "" {
		cmd.CandidateID = cid
	}
	cand, err := s.deps.Runs.Accept(ctx, cmd)
	if err != nil {
		return errResult(err)
	}
	return jsonResult(cand)
}

func (s *Server) handleRejectCandidate(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	id, err := req.RequireString("candidateId")
	if err != nil {
		return errResult(err)
	}
	if err := s.deps.Runs.Reject(ctx, id, req.GetString("reason", "")); err != nil {
		return errResult(err)
	}
	return jsonResult(map[string]any{"ok": true})
}

// ---- helpers ----

func decodeSpec(req mcpgo.CallToolRequest) (domain.TaskSpec, error) {
	var spec domain.TaskSpec
	raw, _ := json.Marshal(req.GetArguments()["spec"])
	if err := json.Unmarshal(raw, &spec); err != nil {
		return spec, fmt.Errorf("decode spec: %w", err)
	}
	return spec, nil
}

type castCardInput struct {
	Stem, Name, Role, CardMarkdown string
}

func strOf(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
