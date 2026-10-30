package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/resource/novel"
)

func TestBuiltinKnowledgeAvailableWithoutLibrary(t *testing.T) {
	root := t.TempDir()
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != len(novel.BuiltinKnowledge()) || len(docs) != 25 {
		t.Fatalf("got %d built-in documents, want 25", len(docs))
	}
	seen := map[string]bool{}
	for _, doc := range docs {
		if !doc.Builtin || doc.Customized || doc.Title == "" || doc.Summary == "" {
			t.Errorf("invalid built-in metadata: %+v", doc)
		}
		if seen[doc.ID] {
			t.Errorf("duplicate document ID: %s", doc.ID)
		}
		seen[doc.ID] = true
		raw, err := Read(root, doc.ID)
		if err != nil || raw == "" {
			t.Errorf("Read(%s) = %q, %v", doc.ID, raw, err)
		}
	}
	for _, id := range []string{"01-pacing-structure", "21-workflow-candidate-accept", "25-block-diagnosis-scene-rescue"} {
		if !seen[id] {
			t.Errorf("missing built-in article %s", id)
		}
	}
}

func TestLocalOverlayAndRestore(t *testing.T) {
	root := t.TempDir()
	const id = "01-pacing-structure"
	original, err := Read(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, id, "# 我的节奏笔记\n\n个人修改。"); err != nil {
		t.Fatal(err)
	}
	raw, _ := Read(root, id)
	if raw != "# 我的节奏笔记\n\n个人修改。" {
		t.Fatalf("overlay not used: %q", raw)
	}
	doc := findDoc(t, root, id)
	if !doc.Builtin || !doc.Customized || doc.Title != "我的节奏笔记" {
		t.Fatalf("unexpected overlay metadata: %+v", doc)
	}
	restored, err := Restore(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if restored != original {
		t.Fatal("restore did not expose the embedded original")
	}
	if _, err := os.Stat(filepath.Join(Root(root), id+".md")); !os.IsNotExist(err) {
		t.Fatalf("restore should remove only the local overlay: %v", err)
	}
	doc = findDoc(t, root, id)
	if doc.Customized {
		t.Fatal("restored built-in is still marked customized")
	}
	// Restoring an untouched document is safe and does not create a directory.
	if _, err := Restore(root, id); err != nil {
		t.Fatal(err)
	}
}

func TestCustomDocumentAndBuiltinsStaySeparate(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "my-book-methods", "# 本书方法\n\n仅本书使用。"); err != nil {
		t.Fatal(err)
	}
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != len(novel.BuiltinKnowledge())+1 {
		t.Fatalf("got %d documents", len(docs))
	}
	doc := findDoc(t, root, "my-book-methods")
	if doc.Builtin || doc.Customized || doc.Category != "custom" {
		t.Fatalf("unexpected custom document: %+v", doc)
	}
	if _, err := Restore(root, "my-book-methods"); !os.IsNotExist(err) {
		t.Fatalf("custom documents cannot be restored as built-ins: %v", err)
	}
	if raw, _ := Read(root, "my-book-methods"); raw == "" {
		t.Fatal("failed custom restore must not delete content")
	}
}

func TestReadRejectsUnknownAndUnsafeIDs(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"", "missing", "../secret", "nested/article", `nested\\article`} {
		if _, err := Read(root, id); err == nil {
			t.Errorf("Read(%q) unexpectedly succeeded", id)
		}
	}
	if err := Write(root, "../secret", "no"); err == nil {
		t.Fatal("unsafe write unexpectedly succeeded")
	}
}

func TestImportPreservesLocalEdits(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "01-pacing-structure", "personal"); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "01-pacing-structure.md"), []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "custom.md"), []byte("# 自定义\n\n正文"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := ImportFromDir(root, source)
	if err != nil || n != 1 {
		t.Fatalf("ImportFromDir = %d, %v", n, err)
	}
	raw, _ := Read(root, "01-pacing-structure")
	if raw != "personal" {
		t.Fatalf("import overwrote personal content: %q", raw)
	}
	if !strings.Contains(mustRead(t, root, "custom"), "自定义") {
		t.Fatal("new custom article was not imported")
	}
}

func findDoc(t *testing.T, root, id string) Doc {
	t.Helper()
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		if doc.ID == id {
			return doc
		}
	}
	t.Fatalf("document %s not found", id)
	return Doc{}
}

func mustRead(t *testing.T, root, id string) string {
	t.Helper()
	raw, err := Read(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
