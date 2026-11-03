package mcp

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/runs"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// create_book 必须与 HTTP 建书一致：最小树 + outline/idea.md。
//
// 旧实现走 CreateBookScaffold，会铺满 9 个空设定模板；那些占位符随后被
// 结构化立项当成既有 canon。这条测试锁住「不再有脚手架设定」这个行为。
func TestCreateBookToolWritesIdeaAndSkipsScaffold(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	srv := New(&Deps{Settings: settings, Store: db, Runs: runs.New(db, settings, llm.NewMock())})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	out := callTool(t, ts, "create_book", map[string]any{
		"bookId": "mcp-idea-book",
		"title":  "雪夜",
		"genre":  "悬疑",
		"idea":   "新警撞见旧案卷宗",
	})
	if !strings.Contains(out, "mcp-idea-book") {
		t.Fatalf("create_book output: %s", out)
	}

	root := filepath.Join(settings.Get().LibraryRoot, "novel", "mcp-idea-book")
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	idea := snap.Files[domain.IdeaPath]
	if !strings.Contains(idea, "新警撞见旧案卷宗") {
		t.Fatalf("idea input missing: %q", idea)
	}
	if strings.TrimSpace(snap.Files["canon/world.md"]) != "" {
		t.Fatal("create_book must not pre-fill empty canon templates")
	}
	if got, _, _ := books.SelectPrimaryAction(snap); got != domain.TaskIdea {
		t.Fatalf("primary action after create = %s, want idea", got)
	}
}
