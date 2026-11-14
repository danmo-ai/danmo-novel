package serve

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDynamicPortAndInProcessContract(t *testing.T) {
	t.Setenv("NOVEL_DB_PATH", "")
	t.Setenv("NOVEL_LIBRARY", "")
	core, err := New(Options{Home: t.TempDir(), Host: "127.0.0.1", DynamicPort: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Start(); err != nil {
		t.Fatal(err)
	}
	if core.BaseURL() == "" || core.BaseURL() == "http://127.0.0.1:7804" {
		t.Fatalf("expected a dynamically allocated port, got %s", core.BaseURL())
	}
	res, err := http.Get(core.BaseURL() + "/api/system/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("health status %d", res.StatusCode)
	}
	if err := core.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultDesktopPortCompatibility(t *testing.T) {
	// The default is still 7804; this test only constructs the core and closes
	// it without binding, so an active developer server does not interfere.
	core, err := New(Options{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer core.Stop(context.Background())
	if core.port != 7804 {
		t.Fatalf("desktop default port changed to %d", core.port)
	}
	if core.BaseURL() != "" {
		t.Fatal("in-process core must not expose a listener")
	}
}

// Enabling iCloud changes only the library root: the registry must be rebuilt
// from the shared folder, existing local books must be copied across once, and
// a second launch must not duplicate or drop them.
func TestSharedLibraryRegistersAndMigratesLocalBooks(t *testing.T) {
	home := t.TempDir()
	localBook := filepath.Join(home, "library", "novel", "local-book")
	if err := os.MkdirAll(filepath.Join(localBook, "canon"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := "title: \"本地\"\ngenre: \"悬疑\"\nstage: write\n"
	if err := os.WriteFile(filepath.Join(localBook, "novel-state.yaml"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}

	shared := t.TempDir()
	t.Setenv("NOVEL_LIBRARY", filepath.Join(shared, "library"))
	t.Setenv("NOVEL_SYNC_DIR", shared)

	for attempt := 0; attempt < 2; attempt++ {
		core, err := New(Options{Home: home, DynamicPort: true})
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		raw := core.Request("GET", "/api/books", "", "")
		if !strings.Contains(raw, "local-book") {
			t.Fatalf("attempt %d: registry lost the migrated book: %s", attempt, raw)
		}
		if err := core.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	moved := filepath.Join(shared, "library", "novel", "local-book", "novel-state.yaml")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("local book was not moved into the shared library: %v", err)
	}
	// A move, not a copy: the local tree is gone, so the two directories cannot
	// look like two different libraries with diverging edits.
	if _, err := os.Stat(filepath.Join(localBook, "novel-state.yaml")); !os.IsNotExist(err) {
		t.Fatal("the local copy survived the move")
	}
	// The shared folder is also where model settings sync writes.
	if _, err := os.Stat(filepath.Join(shared, "model-settings.json")); err != nil {
		t.Fatalf("model settings snapshot was not written: %v", err)
	}

	// Deleting the shared copy must stick: with the local tree gone there is
	// nothing left to resurrect it from.
	if err := os.RemoveAll(filepath.Join(shared, "library", "novel", "local-book")); err != nil {
		t.Fatal(err)
	}
	core, err := New(Options{Home: home, DynamicPort: true})
	if err != nil {
		t.Fatal(err)
	}
	raw := core.Request("GET", "/api/books", "", "")
	if strings.Contains(raw, "local-book") {
		t.Fatalf("a deleted shared book was resurrected: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(shared, "library", "novel", "local-book")); !os.IsNotExist(err) {
		t.Fatal("migration re-created a book deleted from iCloud")
	}
	if err := core.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// The author can point the library anywhere; confirming the change moves every
// book and the knowledge articles, and the registry follows without a restart.
func TestLibraryRootEndpointMovesEverything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_LIBRARY", filepath.Join(home, "library"))
	t.Setenv("NOVEL_SYNC_DIR", "")

	core, err := New(Options{Home: home, DynamicPort: true})
	if err != nil {
		t.Fatal(err)
	}
	defer core.Stop(context.Background())

	draft := `{"bookId":"moved-book","title":"要搬的书","genre":"悬疑"}`
	if raw := core.Request("POST", "/api/books", draft, ""); !strings.Contains(raw, "\"ok\":") && !strings.Contains(raw, "moved-book") {
		t.Fatalf("create book: %s", raw)
	}
	knowledgeDir := filepath.Join(home, "library", "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(knowledgeDir, "style.md"), []byte("我的文风\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "books-here")
	raw := core.Request("PUT", "/api/settings/library-root", `{"path":"`+target+`"}`, "")
	if !strings.Contains(raw, `\"ok\":true`) {
		t.Fatalf("move request failed: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(target, "novel", "moved-book", "novel-state.yaml")); err != nil {
		t.Fatalf("book was not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "knowledge", "style.md")); err != nil {
		t.Fatalf("knowledge was not moved: %v", err)
	}
	shelf := core.Request("GET", "/api/books", "", "")
	if !strings.Contains(shelf, "moved-book") {
		t.Fatalf("registry did not follow the new root: %s", shelf)
	}
	status := core.Request("GET", "/api/sync/status", "", "")
	if !strings.Contains(status, target) {
		t.Fatalf("status does not report the new root: %s", status)
	}
}
