package books

import (
	"os"
	"path/filepath"
	"testing"
)

func writeKnowledge(t *testing.T, libraryRoot, name, body string) {
	t.Helper()
	dir := filepath.Join(libraryRoot, "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMoveLibraryRelocatesBooksAndKnowledge(t *testing.T) {
	from := t.TempDir()
	to := filepath.Join(t.TempDir(), "new-books")
	root := writeBook(t, from, "book-a", "title: \"甲\"\n")
	if err := os.WriteFile(filepath.Join(root, "canon", "world.md"), []byte("世界\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeKnowledge(t, from, "style.md", "作者自己的文风\n")

	report, err := MoveLibrary(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if report.Books != 1 || report.Knowledge != 1 {
		t.Fatalf("report=%+v want one book and one article", report)
	}

	moved, err := os.ReadFile(filepath.Join(to, "novel", "book-a", "canon", "world.md"))
	if err != nil {
		t.Fatalf("book was not moved: %v", err)
	}
	if string(moved) != "世界\n" {
		t.Fatalf("moved content=%q", moved)
	}
	article, err := os.ReadFile(filepath.Join(to, "knowledge", "style.md"))
	if err != nil {
		t.Fatalf("knowledge was not moved: %v", err)
	}
	if string(article) != "作者自己的文风\n" {
		t.Fatalf("knowledge content=%q", article)
	}
	// The old root keeps nothing: leaving books behind would make the two
	// directories look like two libraries.
	if _, err := os.Stat(filepath.Join(from, "novel", "book-a")); !os.IsNotExist(err) {
		t.Fatal("book still present in the old directory")
	}
	if _, err := os.Stat(filepath.Join(from, "knowledge", "style.md")); !os.IsNotExist(err) {
		t.Fatal("knowledge article still present in the old directory")
	}
	if _, err := os.Stat(filepath.Join(from, "novel")); !os.IsNotExist(err) {
		t.Fatal("the emptied novel directory should be removed")
	}
}

// A retried move must finish the job instead of failing or duplicating.
func TestMoveLibraryIsIdempotent(t *testing.T) {
	from := t.TempDir()
	to := filepath.Join(t.TempDir(), "new-books")
	writeBook(t, from, "book-a", "title: \"甲\"\n")
	writeBook(t, from, "book-b", "title: \"乙\"\n")

	if _, err := MoveLibrary(from, to); err != nil {
		t.Fatal(err)
	}
	// Simulate an interrupted move: one book still at the source.
	writeBook(t, from, "book-b", "title: \"乙\"\n")

	report, err := MoveLibrary(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if report.Books != 1 {
		t.Fatalf("report=%+v want only the leftover book", report)
	}
	entries, err := os.ReadDir(filepath.Join(to, "novel"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("target has %d entries, want 2", len(entries))
	}
}

func TestMoveLibraryNeverOverwritesAnExistingBook(t *testing.T) {
	from := t.TempDir()
	to := t.TempDir()
	writeBook(t, from, "book-a", "title: \"甲\"\n")
	writeBook(t, to, "book-a", "title: \"甲（新）\"\n")

	conflicts, err := LibraryConflicts(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || conflicts[0] != "book-a" {
		t.Fatalf("conflicts=%v want book-a", conflicts)
	}

	if _, err := MoveLibrary(from, to); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(filepath.Join(to, "novel", "book-a", "novel-state.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "title: \"甲（新）\"\n" {
		t.Fatalf("target book was overwritten: %q", kept)
	}
}

func TestMoveLibraryRejectsNestedAndRelativeRoots(t *testing.T) {
	from := t.TempDir()
	writeBook(t, from, "book-a", "title: \"甲\"\n")

	if _, err := MoveLibrary(from, filepath.Join(from, "inside")); err == nil {
		t.Fatal("moving a library into itself must fail")
	}
	if _, err := MoveLibrary(from, filepath.Join(filepath.Dir(from), "sibling")); err == nil {
		// sibling of the library is fine; only nesting is rejected
	} else {
		t.Fatal("moving next to the library must be allowed")
	}
	if _, err := MoveLibrary(from, "relative/path"); err == nil {
		t.Fatal("a relative root must fail")
	}
}

func TestMoveLibrarySameRootIsANoop(t *testing.T) {
	root := t.TempDir()
	writeBook(t, root, "book-a", "title: \"甲\"\n")
	report, err := MoveLibrary(root, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Books != 0 || report.Knowledge != 0 {
		t.Fatalf("report=%+v want nothing moved", report)
	}
	if _, err := os.Stat(filepath.Join(root, "novel", "book-a")); err != nil {
		t.Fatalf("book disappeared: %v", err)
	}
}

func TestMoveLibraryMissingSourceIsANoop(t *testing.T) {
	from := filepath.Join(t.TempDir(), "absent")
	to := filepath.Join(t.TempDir(), "new-books")
	report, err := MoveLibrary(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if report.Books != 0 {
		t.Fatalf("report=%+v want nothing moved", report)
	}
}
