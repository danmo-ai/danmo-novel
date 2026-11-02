package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	adaptermcp "github.com/danmo-ai/danmo-novel/core/adapter/mcp"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/knowledge"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/service/runs"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
	"github.com/danmo-ai/danmo-novel/core/version"
	"github.com/gin-gonic/gin"
)

type App struct {
	Settings *config.Manager
	Store    *sqlite.Store
	LLMMgr   *llmconfig.Manager
	Models   *llmconfig.ModelRegistry
	LLM      port.LLMProvider
	Runs     *runs.Service
}

func NewRouter(app *App) *gin.Engine {
	r := gin.Default()
	_ = r.SetTrustedProxies(nil)
	r.Use(cors())
	api := r.Group("/api")
	{
		api.GET("/system/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true, "app": "danmo-novel", "version": version.Version, "time": time.Now().UTC()})
		})
		api.GET("/system/version", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"version": version.Version, "app": "danmo-novel"})
		})
		api.GET("/settings", func(c *gin.Context) { c.JSON(200, app.Settings.Get()) })
		api.GET("/knowledge", func(c *gin.Context) {
			docs, err := knowledge.List(app.Settings.Get().LibraryRoot)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, docs)
		})
		api.GET("/knowledge/stages", func(c *gin.Context) {
			c.JSON(200, knowledge.Stages)
		})
		api.GET("/knowledge/genres", func(c *gin.Context) {
			c.JSON(200, knowledge.GenreFamiliesList)
		})
		api.GET("/knowledge/:id", func(c *gin.Context) {
			root := app.Settings.Get().LibraryRoot
			raw, err := knowledge.Read(root, c.Param("id"))
			if err != nil {
				c.JSON(404, gin.H{"error": "knowledge not found"})
				return
			}
			tags, _ := knowledge.TagsOf(root, c.Param("id"))
			c.JSON(200, gin.H{
				"id": c.Param("id"), "content": raw,
				"stages": nonNil(tags.Stages), "genres": nonNil(tags.Genres), "subgenres": nonNil(tags.Subgenres),
				"default":  tags.Default,
				"audience": tags.Audience,
			})
		})
		api.PUT("/knowledge/:id", func(c *gin.Context) {
			var body struct {
				Content   string    `json:"content"`
				Stages    *[]string `json:"stages"`
				Genres    *[]string `json:"genres"`
				Subgenres *[]string `json:"subgenres"`
				Tags      *struct {
					Stages    []string `json:"stages"`
					Genres    []string `json:"genres"`
					Subgenres []string `json:"subgenres"`
				} `json:"tags"`
			}
			if err := c.BindJSON(&body); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			// No tag field at all means "keep the current tags".
			var keep *knowledge.Tags
			switch {
			case body.Tags != nil:
				// 默认注入是结构属性，不接受客户端覆盖：从当前生效标签继承。
				cur, _ := knowledge.TagsOf(app.Settings.Get().LibraryRoot, c.Param("id"))
				keep = &knowledge.Tags{
					Stages: body.Tags.Stages, Genres: body.Tags.Genres, Subgenres: body.Tags.Subgenres,
					Default: cur.Default,
				}
			case body.Stages != nil || body.Genres != nil || body.Subgenres != nil:
				// Partial update: absent fields keep their current tags.
				tags, _ := knowledge.TagsOf(app.Settings.Get().LibraryRoot, c.Param("id"))
				if body.Stages != nil {
					tags.Stages = *body.Stages
				}
				if body.Genres != nil {
					tags.Genres = *body.Genres
				}
				if body.Subgenres != nil {
					tags.Subgenres = *body.Subgenres
				}
				keep = &tags
			}
			if err := knowledge.Write(app.Settings.Get().LibraryRoot, c.Param("id"), body.Content, keep); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, gin.H{"ok": true})
		})
		api.POST("/knowledge/:id/restore", func(c *gin.Context) {
			root := app.Settings.Get().LibraryRoot
			raw, err := knowledge.Restore(root, c.Param("id"))
			if err != nil {
				c.JSON(404, gin.H{"error": "built-in knowledge not found"})
				return
			}
			tags, _ := knowledge.TagsOf(root, c.Param("id"))
			c.JSON(200, gin.H{
				"id": c.Param("id"), "content": raw,
				"stages": nonNil(tags.Stages), "genres": nonNil(tags.Genres), "subgenres": nonNil(tags.Subgenres),
				"default":  tags.Default,
				"audience": tags.Audience,
			})
		})
		api.PUT("/settings/model-assignments", func(c *gin.Context) {
			var body struct {
				Assignments []domain.ModelAssignment `json:"assignments"`
			}
			if err := c.BindJSON(&body); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			_ = app.Settings.Update(func(s *config.Settings) { s.ModelAssignments = body.Assignments })
			c.JSON(200, app.Settings.Get())
		})
		// 审校分章是唯一的粒度开关；人工审校、审校页重审与「通过」后的定稿都读它，
		// 否则同一个单元会得到不同粒度的 findings。
		api.GET("/settings/review", func(c *gin.Context) {
			c.JSON(200, gin.H{"review": app.Settings.Get().Review})
		})
		api.PUT("/settings/review", func(c *gin.Context) {
			var body struct {
				SplitByChapter bool `json:"splitByChapter"`
				Concurrency    int  `json:"concurrency"`
				Rounds         int  `json:"rounds"`
			}
			if err := c.BindJSON(&body); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			_ = app.Settings.Update(func(s *config.Settings) {
				s.Review.SplitByChapter = body.SplitByChapter
				if body.Concurrency > 0 {
					s.Review.Concurrency = body.Concurrency
				}
				// 轮数是覆盖率与成本的唯一旋钮：1 轮退回旧行为，3 轮是默认。
				// 夹到 1..5，避免 0（等于不审）或离谱值把一次审校变成几十次调用。
				if body.Rounds > 0 {
					s.Review.Rounds = clampInt(body.Rounds, 1, 5)
				}
			})
			c.JSON(200, app.Settings.Get())
		})
		registerLLM(api, app)
		registerBooks(api, app)
		registerRuns(api, app)
	}
	mountMCP(r, app)
	return r
}

// mountMCP exposes streamable-http MCP at /mcp and /mcp/ (trailing slash
// required by Danmo Work's streamable-http dialer).
func mountMCP(r *gin.Engine, app *App) {
	srv := adaptermcp.New(&adaptermcp.Deps{
		Settings: app.Settings,
		Store:    app.Store,
		Runs:     app.Runs,
	})
	h := srv.Handler()
	wrap := func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
	r.Any("/mcp", wrap)
	r.Any("/mcp/*path", wrap)
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func registerLLM(api *gin.RouterGroup, app *App) {
	api.GET("/llm/presets", func(c *gin.Context) { c.JSON(200, app.LLMMgr.Presets()) })
	api.GET("/llm/configs", func(c *gin.Context) {
		cfgs, err := app.LLMMgr.GetAll(c.Request.Context())
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		out := make([]domain.ProviderPublic, 0, len(cfgs))
		for _, cfg := range cfgs {
			out = append(out, cfg.Public())
		}
		c.JSON(200, out)
	})
	api.POST("/llm/configs", func(c *gin.Context) {
		var req domain.UpsertLLMProviderConfigRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		cfg, err := app.LLMMgr.Upsert(c.Request.Context(), req)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, cfg.Public())
	})
	api.PUT("/llm/configs/:id", func(c *gin.Context) {
		var req domain.UpsertLLMProviderConfigRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		existing, _ := app.LLMMgr.GetByID(c.Request.Context(), c.Param("id"))
		if existing.ID == "" {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}
		req.Name = existing.Name
		cfg, err := app.LLMMgr.Upsert(c.Request.Context(), req)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, cfg.Public())
	})
	api.DELETE("/llm/configs/:id", func(c *gin.Context) {
		if err := app.LLMMgr.Delete(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	api.GET("/llm/models", func(c *gin.Context) { c.JSON(200, app.LLMMgr.ListModels(c.Request.Context())) })
	api.GET("/llm/protocol-dialects", func(c *gin.Context) { c.JSON(200, domain.ProtocolDialectInfos) })
	api.GET("/model-configs", func(c *gin.Context) {
		models := app.Settings.Get().DefaultModels
		if models == nil {
			models = []domain.ModelConfig{}
		}
		c.JSON(200, gin.H{"models": models})
	})
	api.PUT("/model-configs", func(c *gin.Context) {
		var models []domain.ModelConfig
		if err := c.BindJSON(&models); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		for i, m := range models {
			if m.Model == "" {
				c.JSON(400, gin.H{"error": fmt.Sprintf("model name required at index %d", i)})
				return
			}
		}
		if err := app.Settings.Update(func(s *config.Settings) { s.DefaultModels = models }); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if app.Models != nil {
			app.Models.SetModels(models)
		}
		c.JSON(200, gin.H{"models": models})
	})
	api.POST("/model-configs/refresh", func(c *gin.Context) {
		builtin := llmconfig.BuiltInDefaultModels()
		if err := app.Settings.Update(func(s *config.Settings) { s.DefaultModels = builtin }); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if app.Models != nil {
			app.Models.SetModels(builtin)
		}
		c.JSON(200, gin.H{"models": builtin})
	})
	api.POST("/llm/configs/fetch-models", func(c *gin.Context) {
		var req domain.UpsertLLMProviderConfigRequest
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		models, err := app.LLMMgr.FetchModelsFromRequest(c.Request.Context(), req)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"models": models})
	})
	api.POST("/llm/configs/:id/refresh-models", func(c *gin.Context) {
		models, err := app.LLMMgr.FetchModels(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"models": models})
	})
	api.PATCH("/llm/configs/:id/models/:modelName", func(c *gin.Context) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		cfg, err := app.LLMMgr.ToggleModel(c.Request.Context(), c.Param("id"), c.Param("modelName"), body.Enabled)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, cfg.Public())
	})
	api.POST("/llm/test", func(c *gin.Context) {
		var body struct {
			ModelID string `json:"modelId"`
		}
		if err := c.BindJSON(&body); err != nil || body.ModelID == "" {
			c.JSON(400, gin.H{"error": "modelId required"})
			return
		}
		start := time.Now()
		resp, err := app.LLM.Chat(c.Request.Context(), port.LLMChatRequest{
			Model:     body.ModelID,
			Messages:  []port.ChatMessage{{Role: "user", Content: "Reply with exactly: ok"}},
			GenParams: &port.ModelGenParams{MaxTokens: 16},
		})
		out := gin.H{"ok": err == nil, "durationMs": time.Since(start).Milliseconds()}
		if err != nil {
			out["error"] = err.Error()
		} else {
			out["content"] = resp.Content
		}
		c.JSON(200, out)
	})
}

func registerBooks(api *gin.RouterGroup, app *App) {
	api.GET("/books", func(c *gin.Context) {
		listed, err := app.Store.ListBooks(c.Request.Context())
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		out := make([]domain.BookSummary, 0, len(listed))
		for _, b := range listed {
			snap, err := books.LoadBook(b.RootPath, b.ReadOnly)
			if err != nil {
				out = append(out, b)
				continue
			}
			open, _ := app.Store.ListOpenCandidates(c.Request.Context(), snap.Summary.ID)
			snap.Summary.OpenCandidates = len(open)
			out = append(out, snap.Summary)
		}
		c.JSON(200, out)
	})
	api.POST("/books", func(c *gin.Context) {
		var body struct{ BookID, Title, Genre string }
		if err := c.BindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if body.BookID == "" {
			c.JSON(400, gin.H{"error": "bookId required"})
			return
		}
		root, err := books.CreateBookScaffold(app.Settings.Get().LibraryRoot, body.BookID, body.Title, body.Genre)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		snap, err := books.LoadBook(root, false)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		_ = app.Store.UpsertBook(c.Request.Context(), snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false)
		c.JSON(200, snap)
	})
	api.POST("/books/import", func(c *gin.Context) {
		var body struct {
			Path     string `json:"path"`
			ReadOnly *bool  `json:"readOnly"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if body.Path == "" {
			c.JSON(400, gin.H{"error": "path required"})
			return
		}
		readOnly := true
		if body.ReadOnly != nil {
			readOnly = *body.ReadOnly
		}
		snap, err := books.LoadBook(body.Path, readOnly)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		_ = app.Store.UpsertBook(c.Request.Context(), snap.Summary.ID, body.Path, snap.Summary.Title, snap.Revision, readOnly)
		c.JSON(200, snap)
	})
	api.GET("/books/:id", func(c *gin.Context) {
		root, readOnly, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		snap, err := books.LoadBook(root, readOnly)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		kind, unit, vol := books.SelectPrimaryAction(snap)
		// drafted 单元的默认下一步是审校；但当已有清单 0 阻断且每章都审完时，
		// 再审一遍只是重跑一次结论不同的审校，单元永远到不了定稿——所以推进到定稿。
		if kind == domain.TaskReview && unit != "" && app.Runs != nil {
			if v, n := app.Runs.ReviewGate(c.Request.Context(), snap.Summary.ID, unit); n > 0 && v == domain.ReviewClean {
				kind = domain.TaskFinalize
			}
		}
		out := gin.H{"book": snap, "primary": gin.H{"kind": kind, "unitId": unit, "volume": vol}}
		// 连续性状态摘要：作者一眼能看出"状态账建到哪了"，缺失时提示先定稿一个单元。
		if states, serr := books.LoadState(root); serr == nil {
			out["continuity"] = books.SummarizeState(states)
		}
		c.JSON(200, out)
	})
	api.POST("/books/:id/scaffold-missing", func(c *gin.Context) {
		root, readOnly, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		if readOnly {
			c.JSON(403, gin.H{"error": "read-only book"})
			return
		}
		snap, err := books.LoadBook(root, false)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := books.ScaffoldFiles(root, snap.Summary.Title, snap.Summary.Genre); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	// 一次性迁移：按已定稿单元的 state_deltas 重建 continuity/state.yaml，
	// 退役 facts.md，把 novel-state 的自由日志搬到 commits/book-notes.md。
	api.POST("/books/:id/migrate-continuity", func(c *gin.Context) {
		root, _, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		rep, err := books.MigrateContinuity(root)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error(), "report": rep})
			return
		}
		c.JSON(200, rep)
	})
	api.POST("/books/:id/enable-write", func(c *gin.Context) {
		root, _, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		snap, err := books.LoadBook(root, false)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		_ = app.Store.UpsertBook(c.Request.Context(), snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false)
		snap.Summary.ReadOnly = false
		c.JSON(200, snap.Summary)
	})
	api.POST("/books/:id/accept-volume", func(c *gin.Context) {
		root, readOnly, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		if readOnly {
			c.JSON(403, gin.H{"error": "read-only book"})
			return
		}
		var body struct {
			Volume int    `json:"volume"`
			Raw    string `json:"raw"`
			Cast   []struct {
				Stem, Name, Role, CardMarkdown string
			} `json:"cast"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if body.Volume < 1 || body.Raw == "" {
			c.JSON(400, gin.H{"error": "volume and raw required"})
			return
		}
		volTag := fmt.Sprintf("v%02d", body.Volume)
		for _, card := range body.Cast {
			dir := filepath.Join(root, "canon", "cast")
			_ = os.MkdirAll(dir, 0o755)
			md := card.CardMarkdown
			if md == "" {
				md = fmt.Sprintf("# %s\n\n`status`: canon\n`role`: %s\n", card.Name, card.Role)
			}
			md = strings.Replace(md, "`status`: candidate", "`status`: canon", 1)
			_ = os.WriteFile(filepath.Join(dir, card.Stem+".md"), []byte(md), 0o644)
		}
		dir := filepath.Join(root, "outline", "volumes")
		_ = os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, volTag+".md"), []byte(body.Raw), 0o644); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true, "volume": volTag})
	})
	api.POST("/books/:id/cast/:stem/promote", func(c *gin.Context) {
		root, readOnly, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		if readOnly {
			c.JSON(403, gin.H{"error": "read-only book"})
			return
		}
		var body struct {
			InitialIdentity *domain.IdentitySegment `json:"initialIdentity"`
			BaseRevision    string                  `json:"baseRevision"`
		}
		_ = c.BindJSON(&body)
		stem := c.Param("stem")
		if stem == "" || strings.Contains(stem, "/") || strings.Contains(stem, "..") {
			c.JSON(400, gin.H{"error": "invalid stem"})
			return
		}
		path := filepath.Join(root, "canon", "cast", stem+".md")
		raw, err := os.ReadFile(path)
		if err != nil {
			c.JSON(404, gin.H{"error": "cast card not found"})
			return
		}
		text := string(raw)
		if !strings.Contains(text, "`status`: candidate") {
			c.JSON(400, gin.H{"error": "only candidate cards can be promoted"})
			return
		}
		// 晋升前要求最基础的内容：三锚点或知识边界至少一项非空。
		if !strings.Contains(text, "视觉") && !strings.Contains(text, "不知") {
			c.JSON(400, gin.H{"error": "请先补全人物卡（三锚点 / 知识边界），再晋升 canon"})
			return
		}
		if body.InitialIdentity == nil {
			c.JSON(422, gin.H{"error": "晋升 canon 前请登记初始职务身份"})
			return
		}
		snap, err := books.LoadBook(root, false)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if body.BaseRevision != "" && body.BaseRevision != snap.Revision {
			c.JSON(409, gin.H{"error": "书籍已更新，请重新确认人物与身份"})
			return
		}
		identity := *body.InitialIdentity
		identity.Stem = stem
		if identity.FromUnit == "" {
			identity.FromUnit = "v01-U1"
		}
		segments, err := books.ApplyIdentityChange(snap.IdentitySegments, identity, snap.Units)
		if err != nil {
			c.JSON(422, gin.H{"error": err.Error()})
			return
		}
		identityBody, err := books.MarshalIdentityTimeline(segments)
		if err != nil {
			c.JSON(422, gin.H{"error": err.Error()})
			return
		}
		backup, err := os.ReadFile(filepath.Join(root, books.IdentityTimelinePath))
		if err != nil && !os.IsNotExist(err) {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := os.WriteFile(filepath.Join(root, books.IdentityTimelinePath), identityBody, 0o644); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		text = strings.Replace(text, "`status`: candidate", "`status`: canon", 1)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			if len(backup) > 0 {
				_ = os.WriteFile(filepath.Join(root, books.IdentityTimelinePath), backup, 0o644)
			}
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true, "stem": stem, "status": "canon"})
	})
	api.POST("/books/:id/identity-segments", func(c *gin.Context) {
		var body struct {
			Action       string                 `json:"action"` // append | update | close
			Index        int                    `json:"index"`
			Segment      domain.IdentitySegment `json:"segment"`
			BaseRevision string                 `json:"baseRevision"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		root, readOnly, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		if readOnly {
			c.JSON(403, gin.H{"error": "read-only book"})
			return
		}
		snap, err := books.LoadBook(root, false)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if body.BaseRevision == "" || body.BaseRevision != snap.Revision {
			c.JSON(409, gin.H{"error": "人物身份已更新，请重新比较后再保存"})
			return
		}
		var segments []domain.IdentitySegment
		switch body.Action {
		case "append":
			segments, err = books.ApplyIdentityChange(snap.IdentitySegments, body.Segment, snap.Units)
		case "update":
			segments, err = books.UpdateIdentitySegment(snap.IdentitySegments, body.Index, body.Segment, snap.Units)
		case "close":
			segments, err = books.CloseIdentitySegment(snap.IdentitySegments, body.Index, body.Segment.ToUnit, snap.Units)
		default:
			err = fmt.Errorf("未知身份维护操作")
		}
		if err != nil {
			c.JSON(422, gin.H{"error": err.Error()})
			return
		}
		if err := books.SaveIdentityTimeline(root, segments); err != nil {
			c.JSON(422, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true, "segments": segments})
	})
	api.GET("/books/:id/file", func(c *gin.Context) {
		rel := c.Query("path")
		if rel == "" || strings.Contains(rel, "..") {
			c.JSON(400, gin.H{"error": "invalid path"})
			return
		}
		root, _, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"path": rel, "content": string(b)})
	})
	api.PATCH("/books/:id/outline-field", func(c *gin.Context) {
		var body struct {
			Path, Kind, EntityID, Field, Value string
			BaseRevision                       string `json:"baseRevision"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		root, readOnly, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		if readOnly {
			c.JSON(403, gin.H{"error": "read-only book"})
			return
		}
		if (body.Path != "outline/book_outline.md" && !strings.HasPrefix(body.Path, "outline/volumes/")) || strings.Contains(body.Path, "..") {
			c.JSON(400, gin.H{"error": "invalid outline path"})
			return
		}
		raw, err := os.ReadFile(filepath.Join(root, body.Path))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		protocol := books.BookOutlineProtocol
		if body.Path != "outline/book_outline.md" {
			protocol = books.VolumeOutlineProtocol
		}
		updated, err := books.ReplaceMarkdownField(string(raw), protocol, body.Kind, body.EntityID, body.Field, body.Value)
		if err != nil {
			c.JSON(422, gin.H{"error": err.Error()})
			return
		}
		snap, err := books.LoadBook(root, false)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := books.ValidateStructuredOutlineFile(snap, body.Path, updated); err != nil {
			c.JSON(422, gin.H{"error": err.Error()})
			return
		}
		if body.BaseRevision != "" && body.BaseRevision != snap.Revision {
			c.JSON(409, gin.H{"error": "大纲已被其他修改更新，请重新比较后再保存"})
			return
		}
		if err := os.WriteFile(filepath.Join(root, body.Path), []byte(updated), 0o644); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true, "content": updated})
	})
	api.PUT("/books/:id/file", func(c *gin.Context) {
		var body struct {
			Path, Content string
			BaseRevision  string `json:"baseRevision"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if body.Path == "" || strings.Contains(body.Path, "..") {
			c.JSON(400, gin.H{"error": "invalid path"})
			return
		}
		if body.Path == books.IdentityTimelinePath {
			c.JSON(422, gin.H{"error": "身份时间线请通过人物卡的身份段维护或规划候选更新"})
			return
		}
		root, readOnly, err := app.Store.GetBookRoot(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		if readOnly {
			c.JSON(403, gin.H{"error": "read-only book"})
			return
		}
		if body.Path == "outline/book_outline.md" || strings.HasPrefix(body.Path, "outline/volumes/") {
			doc := books.ParseStructuredMarkdown(body.Content, books.BookOutlineProtocol)
			if doc.Legacy {
				c.JSON(422, gin.H{"error": "旧格式大纲不进入生产链路，请先通过 Agent 批量迁移"})
				return
			}
			snap, loadErr := books.LoadBook(root, false)
			if loadErr != nil {
				c.JSON(500, gin.H{"error": loadErr.Error()})
				return
			}
			if err := books.ValidateStructuredOutlineFile(snap, body.Path, body.Content); err != nil {
				c.JSON(422, gin.H{"error": err.Error()})
				return
			}
			if body.BaseRevision != "" && body.BaseRevision != snap.Revision {
				c.JSON(409, gin.H{"error": "大纲已被其他修改更新，请重新比较后再保存"})
				return
			}
		}
		full := filepath.Join(root, body.Path)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body.Content), 0o644); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
}

func registerRuns(api *gin.RouterGroup, app *App) {
	api.POST("/runs/preview", func(c *gin.Context) {
		var spec domain.TaskSpec
		if err := c.BindJSON(&spec); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		packet, err := app.Runs.PreviewPacket(c.Request.Context(), spec)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, packet)
	})
	api.POST("/runs", func(c *gin.Context) {
		var spec domain.TaskSpec
		if err := c.BindJSON(&spec); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		run, err := app.Runs.Start(c.Request.Context(), spec)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error(), "run": run})
			return
		}
		c.JSON(200, run)
	})
	api.GET("/runs", func(c *gin.Context) {
		list, err := app.Store.ListRuns(c.Request.Context(), c.Query("bookId"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, list)
	})
	api.GET("/runs/:id", func(c *gin.Context) {
		run, err := app.Store.GetRun(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, run)
	})
	api.GET("/books/:id/runs", func(c *gin.Context) {
		list, err := app.Store.ListRuns(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, list)
	})
	api.POST("/runs/:id/cancel", func(c *gin.Context) {
		if err := app.Runs.Cancel(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	api.GET("/candidates/:id", func(c *gin.Context) {
		cand, err := app.Store.GetCandidate(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, cand)
	})
	api.GET("/books/:id/candidates", func(c *gin.Context) {
		list, err := app.Store.ListOpenCandidates(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, list)
	})
	// 已终结候选（含「接受归档」的审校清单）：只读回看，不参与任何流程判断。
	api.GET("/books/:id/candidates/history", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		list, err := app.Store.ListClosedCandidates(c.Request.Context(), c.Param("id"), limit)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		// 审校候选把被审正文复制进了 chapters；历史列表只回看结论，顺手剥掉以免响应过大。
		for i := range list {
			list[i].Chapters = nil
			list[i].PlanSettings = nil
			list[i].PlanMarkdown = nil
			list[i].Replacement = ""
		}
		c.JSON(200, list)
	})
	api.POST("/candidates/:id/accept", func(c *gin.Context) {
		var body domain.AcceptCommand
		_ = c.BindJSON(&body)
		body.CandidateID = c.Param("id")
		cand, err := app.Runs.Accept(c.Request.Context(), body)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, cand)
	})
	// 审校结案：归档该单元全部待处理清单，并按需直接起定稿。
	// force 由作者在确认框里授权；不带 force 时服务端拒绝带阻断/不完整的清单。
	api.POST("/books/:id/units/:unitId/close-review", func(c *gin.Context) {
		var body struct {
			Force         bool  `json:"force"`
			StartFinalize *bool `json:"startFinalize"`
		}
		_ = c.BindJSON(&body)
		startFinalize := true
		if body.StartFinalize != nil {
			startFinalize = *body.StartFinalize
		}
		res, err := app.Runs.CloseUnitReview(c.Request.Context(), c.Param("id"), c.Param("unitId"), body.Force, startFinalize)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error(), "verdict": res.Verdict, "closed": res.Closed})
			return
		}
		c.JSON(200, res)
	})
	api.POST("/candidates/:id/reject", func(c *gin.Context) {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = c.BindJSON(&body)
		if err := app.Runs.Reject(c.Request.Context(), c.Param("id"), body.Reason); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
}

// nonNil keeps JSON shape stable: an untagged article serializes as [].
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// clampInt bounds a user-supplied numeric setting.
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
