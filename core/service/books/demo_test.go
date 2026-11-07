package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 示例书是 App Store 首启体验的全部依据：审核员不配模型也能看到完整成品。
// 所以它不是"随便放几个文件"，必须真的能通过生产链路的校验并且状态自洽。
func TestDemoBookPassesProductionValidation(t *testing.T) {
	lib := t.TempDir()
	root, created, err := CreateDemoBook(lib)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first call should create")
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}

	if snap.Summary.Title != "雾港夜航" {
		t.Fatalf("title=%q", snap.Summary.Title)
	}
	// 四层都要在：总纲、设定、卷纲、单元正文。
	if strings.TrimSpace(snap.Files["outline/book_outline.md"]) == "" {
		t.Fatal("missing book outline")
	}
	if strings.TrimSpace(snap.Files["outline/volumes/v01.md"]) == "" {
		t.Fatal("missing volume outline")
	}
	for _, p := range []string{"book-bible.md", "canon/world.md", "canon/goldfinger.md", "canon/style-fingerprint.md"} {
		if strings.TrimSpace(snap.Files[p]) == "" {
			t.Fatalf("missing setting %s", p)
		}
	}
	if len(snap.Cast) != 3 {
		t.Fatalf("cast=%d, want 3", len(snap.Cast))
	}
	if len(snap.Units) != 2 {
		t.Fatalf("units=%d, want 2", len(snap.Units))
	}
	if !snap.Units[0].HasProse {
		t.Fatalf("v01-U1 should carry prose, got %+v", snap.Units[0])
	}
	if snap.Units[1].HasProse {
		t.Fatal("v01-U2 must stay without prose so the demo shows both states")
	}
	if snap.Units[0].Status != domain.UnitDrafted {
		t.Fatalf("v01-U1 status=%s, want drafted", snap.Units[0].Status)
	}

	// 两份结构化大纲必须过与候选 Accept 同一套校验，否则示例书在真实流程里
	// 会被判成"旧格式"或字段缺失——那正是它想演示的东西。
	for _, p := range []string{"outline/book_outline.md", "outline/volumes/v01.md"} {
		if err := ValidateStructuredOutlineFile(snap, p, snap.Files[p]); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}

	if kind, unit, _ := SelectPrimaryAction(snap); kind == "" || unit == "" {
		t.Fatalf("primary action unresolved: %s/%s", kind, unit)
	}
}

// 重复点击"看示例"不能造出第二本，也不能覆盖作者改过的内容。
func TestDemoBookIsIdempotent(t *testing.T) {
	lib := t.TempDir()
	root, created, err := CreateDemoBook(lib)
	if err != nil || !created {
		t.Fatalf("first call: created=%v err=%v", created, err)
	}
	edited := filepath.Join(root, "canon", "world.md")
	if err := os.WriteFile(edited, []byte("# 我改过的设定\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root2, created2, err := CreateDemoBook(lib)
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Fatal("second call must not recreate")
	}
	if root2 != root {
		t.Fatalf("root=%q want %q", root2, root)
	}
	body, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "我改过的设定") {
		t.Fatal("author edits were overwritten")
	}
}
