// Package serve owns the embeddable Danmo Novel application core.
package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	httpapi "github.com/danmo-ai/danmo-novel/core/adapter/http"
	llmadapt "github.com/danmo-ai/danmo-novel/core/adapter/llm"
	adaptermcp "github.com/danmo-ai/danmo-novel/core/adapter/mcp"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/service/runs"
	"github.com/danmo-ai/danmo-novel/core/service/settingsync"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

type Options struct {
	Home string
	Host string
	Port int
	// DynamicPort requests an OS-assigned port, including for a desktop home.
	DynamicPort bool
}

type Server struct {
	app     *httpapi.App
	handler http.Handler
	home    string
	host    string
	port    int

	mu       sync.Mutex
	listener net.Listener
	http     *http.Server
	serveErr chan error
	stopping bool
}

// New constructs a core without opening a network listener. Handler may be used
// in-process on mobile, keeping the exact same routes as desktop and browser.
func New(opts Options) (*Server, error) {
	home := opts.Home
	if home == "" {
		home = runtimepaths.Home()
	}
	if err := runtimepaths.SetHome(home); err != nil {
		return nil, err
	}
	settings, err := config.Load()
	if err != nil {
		return nil, err
	}
	db, err := sqlite.Open(runtimepaths.DBPath())
	if err != nil {
		return nil, err
	}
	// Always re-read the on-disk library. It may live in iCloud and be changed
	// by another device, so the registry's stored absolute paths are never
	// authoritative — on desktop as well as in the embedded workspace.
	if err := reconcileLibrary(context.Background(), db, settings, home); err != nil {
		_ = db.Close()
		return nil, err
	}
	models := llmconfig.NewModelRegistry()
	models.SetModels(settings.Get().DefaultModels)
	llmMgr := llmconfig.NewManager(db, models)
	provider := llmadapt.NewDefaultLLMProvider(llmMgr, models)
	runSvc := runs.New(db, settings, provider)
	runSvc.Models = models
	if n, err := runSvc.RecoverInterrupted(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover interrupted runs: %w", err)
	} else if n > 0 {
		log.Printf("recovered %d interrupted run(s) from previous process", n)
	}
	app := &httpapi.App{Settings: settings, Store: db, LLMMgr: llmMgr, Models: models, LLM: provider, Runs: runSvc}
	// Model settings follow the books into the shared folder when the host app
	// provides one. Pull first so this device sees the other device's changes,
	// then push so the snapshot always reflects a state this app started from.
	syncSvc := settingsync.New(runtimepaths.SyncDir(), llmMgr, settings)
	app.Sync = syncSvc
	if syncSvc.Enabled() {
		syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if _, err := syncSvc.Sync(syncCtx); err != nil {
			log.Printf("warning: model settings sync: %v", err)
		}
		cancel()
		// A pulled settings block may carry a different model catalog; the
		// registry was built before the merge, so re-seed it.
		models.SetModels(settings.Get().DefaultModels)
	}
	host := opts.Host
	if host == "" {
		host = first(os.Getenv("DQ_HTTP_HOST"), "127.0.0.1")
	}
	webRoot := os.Getenv("NOVEL_WEB_DIST")
	if webRoot == "" {
		webRoot = filepath.Join(filepath.Dir(os.Args[0]), "web")
	}
	app.WebRoot = webRoot
	port := opts.Port
	// New is in-process on mobile and has no listener; port 0 there must not
	// inherit a developer's desktop DQ_BACKEND_PORT.
	if port == 0 && !opts.DynamicPort {
		if raw := first(os.Getenv("DQ_BACKEND_PORT"), os.Getenv("DANMO_NOVEL_PORT")); raw != "" {
			port, _ = strconv.Atoi(raw)
		}
		if port == 0 && host != "0.0.0.0" {
			port = 7804
		}
	}
	return &Server{app: app, handler: httpapi.NewRouter(app), home: home, host: host, port: port}, nil
}

// reconcileLibrary makes the SQLite registry match the books on disk.
//
// When the platform default moved (macOS adopting iCloud once the container
// exists) books left in the local home are relocated first, so the author's
// existing shelf follows the new default instead of looking empty. The move is
// the same one the "change book directory" setting uses: idempotent, never
// overwriting, so a partially migrated library is finished on the next launch.
func reconcileLibrary(ctx context.Context, db *sqlite.Store, settings *config.Manager, home string) error {
	libraryRoot := settings.Get().LibraryRoot
	localRoot := filepath.Join(home, "library")
	if filepath.Clean(libraryRoot) != filepath.Clean(localRoot) {
		if report, err := books.MoveLibrary(localRoot, libraryRoot); err != nil {
			// A container that cannot be written must not stop startup; the
			// remaining books stay local and the move retries next launch.
			log.Printf("warning: move local books into %s: %v", libraryRoot, err)
		} else if report.Books > 0 || report.Knowledge > 0 {
			log.Printf("moved %d book(s) and %d knowledge article(s) into %s",
				report.Books, report.Knowledge, libraryRoot)
		}
	}
	return books.ReconcileRegistry(ctx, db, libraryRoot)
}

func (s *Server) Handler() http.Handler { return s.handler }
func (s *Server) Home() string          { return s.home }

// Request executes one API request directly in the current process. It is the
// mobile FFI transport: no listener, port race, or local-network permission.
func (s *Server) Request(method, path, body, authorization string) string {
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	res := httptest.NewRecorder()
	s.handler.ServeHTTP(res, req)
	payload := map[string]any{"status": res.Code, "body": res.Body.String()}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return nil
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
	if err != nil {
		return err
	}
	s.listener = ln
	s.http = &http.Server{Handler: s.handler, ReadHeaderTimeout: 10 * time.Second}
	s.serveErr = make(chan error, 1)
	if err := adaptermcp.WriteAPIPort(ln.Addr().String()); err != nil {
		log.Printf("warning: write api.port: %v", err)
	}
	log.Printf("Danmo Novel API http://%s home=%s", ln.Addr(), s.home)
	go func() {
		err := s.http.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.serveErr <- err
		}
		close(s.serveErr)
	}()
	return nil
}

func (s *Server) BaseURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return "http://" + s.listener.Addr().String()
}

func (s *Server) Wait() error {
	s.mu.Lock()
	ch := s.serveErr
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	return <-ch
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return nil
	}
	s.stopping = true
	srv := s.http
	s.mu.Unlock()
	var stopErr error
	if s.app.Runs != nil {
		stopErr = s.app.Runs.Shutdown(ctx)
	}
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil && stopErr == nil {
			stopErr = err
		}
	}
	if err := s.app.Store.Close(); err != nil && stopErr == nil {
		stopErr = err
	}
	return stopErr
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
