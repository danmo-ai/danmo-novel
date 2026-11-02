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
	s.mcp.AddTool(mcpgo.NewTool("health",
		mcpgo.WithDescription("Danmo Novel server health: app name, version, ok."),
	), s.handleHealth)

	s.mcp.AddTool(mcpgo.NewTool("list_books",
		mcpgo.WithDescription("List all books on the shelf with summaries."),
	), s.handleListBooks)

	s.mcp.AddTool(mcpgo.NewTool("get_book",
		mcpgo.WithDescription("Get one book snapshot plus the computed primary next action."),
		mcpgo.WithString("bookId", mcpgo.Required(), mcpgo.Description("Book id")),
	), s.handleGetBook)

	s.mcp.AddTool(mcpgo.NewTool("create_book",
		mcpgo.WithDescription("Create a new book scaffold (立项) under the library."),
		mcpgo.WithString("bookId", mcpgo.Required(), mcpgo.Description("Slug, e.g. my-book")),
		mcpgo.WithString("title", mcpgo.Description("Book title")),
		mcpgo.WithString("genre", mcpgo.Description("Genre label")),
	), s.handleCreateBook)

	s.mcp.AddTool(mcpgo.NewTool("import_book",
		mcpgo.WithDescription("Import an existing book tree with the standard structure."),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("Absolute path to the book root")),
		mcpgo.WithBoolean("readOnly", mcpgo.Description("Import read-only (default true)")),
	), s.handleImportBook)

	s.mcp.AddTool(mcpgo.NewTool("read_book_file",
		mcpgo.WithDescription("Read a single file from a book tree (relative path)."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("Book-relative path, e.g. outline/book_outline.md")),
	), s.handleReadBookFile)

	s.mcp.AddTool(mcpgo.NewTool("write_book_file",
		mcpgo.WithDescription("Write a single file in a book tree (author fill-in only; prose/canon still go through candidates + accept)."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithString("path", mcpgo.Required()),
		mcpgo.WithString("content", mcpgo.Required()),
	), s.handleWriteBookFile)

	s.mcp.AddTool(mcpgo.NewTool("accept_volume",
		mcpgo.WithDescription("Human gate: accept an approved volume outline + promote candidate cast cards for the volume."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithNumber("volume", mcpgo.Required(), mcpgo.Description("Volume number, e.g. 1")),
		mcpgo.WithString("raw", mcpgo.Required(), mcpgo.Description("Approved volume outline markdown")),
		mcpgo.WithArray("cast", mcpgo.Description("Optional cast cards to promote: [{stem,name,role,cardMarkdown}]")),
	), s.handleAcceptVolume)

	s.mcp.AddTool(mcpgo.NewTool("promote_cast",
		mcpgo.WithDescription("Promote one candidate cast card to canon."),
		mcpgo.WithString("bookId", mcpgo.Required()),
		mcpgo.WithString("stem", mcpgo.Required()),
	), s.handlePromoteCast)

	s.mcp.AddTool(mcpgo.NewTool("preview_run",
		mcpgo.WithDescription("Build the context packet for a TaskSpec without starting a run."),
		mcpgo.WithObject("spec", mcpgo.Required(), mcpgo.Description("TaskSpec JSON (kind, bookId, volumeId/unitId, mode, knowledgeIds, …)")),
	), s.handlePreviewRun)

	s.mcp.AddTool(mcpgo.NewTool("start_run",
		mcpgo.WithDescription("Start a controlled generation run for a TaskSpec. Blocks until done; returns the run. No tools are sent to the LLM."),
		mcpgo.WithObject("spec", mcpgo.Required(), mcpgo.Description("TaskSpec JSON")),
	), s.handleStartRun)

	s.mcp.AddTool(mcpgo.NewTool("get_run",
		mcpgo.WithDescription("Get a run by id."),
		mcpgo.WithString("runId", mcpgo.Required()),
	), s.handleGetRun)

	s.mcp.AddTool(mcpgo.NewTool("list_runs",
		mcpgo.WithDescription("List runs, optionally filtered by bookId."),
		mcpgo.WithString("bookId", mcpgo.Description("Optional book filter")),
	), s.handleListRuns)

	s.mcp.AddTool(mcpgo.NewTool("cancel_run",
		mcpgo.WithDescription("Cancel a running run."),
		mcpgo.WithString("runId", mcpgo.Required()),
	), s.handleCancelRun)

	s.mcp.AddTool(mcpgo.NewTool("list_candidates",
		mcpgo.WithDescription("List open candidates for a book."),
		mcpgo.WithString("bookId", mcpgo.Required()),
	), s.handleListCandidates)

	s.mcp.AddTool(mcpgo.NewTool("get_candidate",
		mcpgo.WithDescription("Get one candidate by id."),
		mcpgo.WithString("candidateId", mcpgo.Required()),
	), s.handleGetCandidate)

	s.mcp.AddTool(mcpgo.NewTool("accept_candidate",
		mcpgo.WithDescription("Accept a candidate: the ONLY path that writes canon/prose and advances status."),
		mcpgo.WithObject("command", mcpgo.Required(), mcpgo.Description("AcceptCommand JSON (baseRevision, acceptFindingIds, forceRiskAccept)")),
		mcpgo.WithString("candidateId", mcpgo.Description("Candidate id (may also be inside command)")),
	), s.handleAcceptCandidate)

	s.mcp.AddTool(mcpgo.NewTool("reject_candidate",
		mcpgo.WithDescription("Reject a candidate with a reason."),
		mcpgo.WithString("candidateId", mcpgo.Required()),
		mcpgo.WithString("reason", mcpgo.Description("Reason")),
	), s.handleRejectCandidate)
}

// ---- handlers ----

func (s *Server) handleHealth(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return jsonResult(map[string]any{"ok": true, "app": "danmo-novel", "version": version.Version})
}

func (s *Server) handleListBooks(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	listed, err := s.deps.Store.ListBooks(ctx)
	if err != nil {
		return errResult(err)
	}
	out := make([]domain.BookSummary, 0, len(listed))
	for _, b := range listed {
		snap, err := books.LoadBook(b.RootPath, b.ReadOnly)
		if err != nil {
			out = append(out, b)
			continue
		}
		open, _ := s.deps.Store.ListOpenCandidates(ctx, snap.Summary.ID)
		snap.Summary.OpenCandidates = len(open)
		out = append(out, snap.Summary)
	}
	return jsonResult(out)
}

func (s *Server) handleGetBook(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
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
	kind, unit, vol := books.SelectPrimaryAction(snap)
	// 与 HTTP 侧同一条规则：审校已干净时下一步是定稿，不是再审一遍。
	if kind == domain.TaskReview && unit != "" && s.deps.Runs != nil {
		if v, n := s.deps.Runs.ReviewGate(ctx, snap.Summary.ID, unit); n > 0 && v == domain.ReviewClean {
			kind = domain.TaskFinalize
		}
	}
	return jsonResult(map[string]any{
		"book":    snap,
		"primary": map[string]any{"kind": kind, "unitId": unit, "volume": vol},
	})
}

func (s *Server) handleCreateBook(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	if bookID == "" {
		return errResult(fmt.Errorf("bookId required"))
	}
	root, err := books.CreateBookScaffold(s.deps.Settings.Get().LibraryRoot, bookID, req.GetString("title", ""), req.GetString("genre", ""))
	if err != nil {
		return errResult(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		return errResult(err)
	}
	_ = s.deps.Store.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false)
	return jsonResult(snap)
}

func (s *Server) handleImportBook(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	path, err := req.RequireString("path")
	if err != nil {
		return errResult(err)
	}
	readOnly := req.GetBool("readOnly", true)
	snap, err := books.LoadBook(path, readOnly)
	if err != nil {
		return errResult(err)
	}
	_ = s.deps.Store.UpsertBook(ctx, snap.Summary.ID, path, snap.Summary.Title, snap.Revision, readOnly)
	return jsonResult(snap)
}

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

func (s *Server) handleAcceptVolume(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	volume, err := req.RequireInt("volume")
	if err != nil {
		return errResult(err)
	}
	raw, err := req.RequireString("raw")
	if err != nil {
		return errResult(err)
	}
	var cast []castCardInput
	if args := req.GetArguments(); args != nil {
		if arr, ok := args["cast"].([]any); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					cast = append(cast, castCardInput{
						Stem:         strOf(m, "stem"),
						Name:         strOf(m, "name"),
						Role:         strOf(m, "role"),
						CardMarkdown: strOf(m, "cardMarkdown"),
					})
				}
			}
		}
	}
	volTag, err := acceptVolume(ctx, s.deps.Store, bookID, volume, raw, cast)
	if err != nil {
		return errResult(err)
	}
	return jsonResult(map[string]any{"ok": true, "volume": volTag})
}

func (s *Server) handlePromoteCast(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	stem, err := req.RequireString("stem")
	if err != nil {
		return errResult(err)
	}
	if err := promoteCast(ctx, s.deps.Store, bookID, stem); err != nil {
		return errResult(err)
	}
	return jsonResult(map[string]any{"ok": true, "stem": stem, "status": "canon"})
}

func (s *Server) handlePreviewRun(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	spec, err := decodeSpec(req)
	if err != nil {
		return errResult(err)
	}
	packet, err := s.deps.Runs.PreviewPacket(ctx, spec)
	if err != nil {
		return errResult(err)
	}
	return jsonResult(packet)
}

func (s *Server) handleStartRun(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	spec, err := decodeSpec(req)
	if err != nil {
		return errResult(err)
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

func (s *Server) handleListRuns(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	list, err := s.deps.Store.ListRuns(ctx, req.GetString("bookId", ""))
	if err != nil {
		return errResult(err)
	}
	return jsonResult(list)
}

func (s *Server) handleCancelRun(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	runID, err := req.RequireString("runId")
	if err != nil {
		return errResult(err)
	}
	if err := s.deps.Runs.Cancel(ctx, runID); err != nil {
		return errResult(err)
	}
	return jsonResult(map[string]any{"ok": true})
}

func (s *Server) handleListCandidates(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	list, err := s.deps.Store.ListOpenCandidates(ctx, bookID)
	if err != nil {
		return errResult(err)
	}
	return jsonResult(list)
}

func (s *Server) handleGetCandidate(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	id, err := req.RequireString("candidateId")
	if err != nil {
		return errResult(err)
	}
	cand, err := s.deps.Store.GetCandidate(ctx, id)
	if err != nil {
		return errResult(err)
	}
	return jsonResult(cand)
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
