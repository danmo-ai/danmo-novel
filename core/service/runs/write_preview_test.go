package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// 确认弹窗里的上下文就是真正会发出去的那一份：选了「在原文上优化」，预览
// 必须能看到旧稿块；选「重新生成」则一块都不能有。
func TestWritePreviewReflectsRewriteMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	root := filepath.Join(home, "book")
	src, _ := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan"))
	copyBookDir(t, src, root)
	prose := "## 第1章 夜\n\n林尘没有回头。\n\n## 第2章 门\n\n石门开了。\n\n## 第3章 影\n\n影子退去。"
	if err := os.MkdirAll(filepath.Join(root, "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "units", "v01-U1.md"), []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	svc := New(db, settings, nil)
	unitID := snap.Units[0].UnitID

	originalBlock := func(p domain.ContextPacket) string {
		payload, err := contextsvc.DecodeUserPayload(p.UserPrompt)
		if err != nil {
			t.Fatalf("decode user payload: %v", err)
		}
		for _, d := range payload.Documents {
			if d.ID == "original-prose" {
				return d.Content
			}
		}
		return ""
	}

	fresh, err := svc.PreviewPacket(ctx, domain.TaskSpec{Kind: domain.TaskWrite, BookID: snap.Summary.ID, UnitID: unitID, RewriteMode: domain.RewriteFresh})
	if err != nil {
		t.Fatalf("fresh preview: %v", err)
	}
	if got := originalBlock(fresh); got != "" {
		t.Fatalf("fresh preview must not carry the draft, got %q", got)
	}

	refine, err := svc.PreviewPacket(ctx, domain.TaskSpec{Kind: domain.TaskWrite, BookID: snap.Summary.ID, UnitID: unitID, RewriteMode: domain.RewriteRefine})
	if err != nil {
		t.Fatalf("refine preview: %v", err)
	}
	if got := originalBlock(refine); !strings.Contains(got, "林尘没有回头") {
		t.Fatalf("refine preview must carry the draft, got %q", got)
	}
}
