package books

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

type fakeRegistry struct {
	books map[string]domain.BookSummary
}

func newFakeRegistry(books ...domain.BookSummary) *fakeRegistry {
	r := &fakeRegistry{books: map[string]domain.BookSummary{}}
	for _, b := range books {
		r.books[b.ID] = b
	}
	return r
}

func (r *fakeRegistry) ListBooks(context.Context) ([]domain.BookSummary, error) {
	out := make([]domain.BookSummary, 0, len(r.books))
	for _, b := range r.books {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *fakeRegistry) UpsertBook(_ context.Context, id, root, title, revision string, readOnly bool) error {
	r.books[id] = domain.BookSummary{ID: id, RootPath: root, Title: title, ReadOnly: readOnly}
	return nil
}

func (r *fakeRegistry) DeleteBook(_ context.Context, id string) error {
	delete(r.books, id)
	return nil
}

func writeBook(t *testing.T, libraryRoot, id, state string) string {
	t.Helper()
	root := filepath.Join(libraryRoot, "novel", id)
	if err := os.MkdirAll(filepath.Join(root, "canon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "novel-state.yaml"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestScanLibraryFindsOnlyBookDirectories(t *testing.T) {
	libraryRoot := t.TempDir()
	writeBook(t, libraryRoot, "book-a", "title: \"甲\"\ngenre: \"悬疑\"\nstage: write\n")
	// A Bible-only book is still a book: the draft tree may lack state briefly.
	bibleRoot := filepath.Join(libraryRoot, "novel", "book-b")
	if err := os.MkdirAll(bibleRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bibleRoot, "book-bible.md"), []byte("# 乙\n\n正文设定\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not books: no markers, or a hidden directory (iCloud puts `.Trash` style
	// entries next to user data).
	for _, name := range []string{"not-a-book", ".Trash"} {
		if err := os.MkdirAll(filepath.Join(libraryRoot, "novel", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(libraryRoot, "novel", "loose.md"), []byte("# x"), 0o644); err != nil {
		t.Fatal(err)
	}

	books, err := ScanLibrary(libraryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("books=%+v want two", books)
	}
	if books[0].ID != "book-a" || books[0].Title != "甲" || books[0].Genre != "悬疑" {
		t.Fatalf("first book=%+v want the state file values", books[0])
	}
	if books[1].ID != "book-b" || books[1].Title != "乙" {
		t.Fatalf("second book=%+v want the Bible heading as title", books[1])
	}
}

func TestScanLibraryMissingRootIsEmpty(t *testing.T) {
	books, err := ScanLibrary(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 0 {
		t.Fatalf("books=%+v want none", books)
	}
}

// The same book id may point at a stale path (another device, another
// container); reconcile must rewrite it and drop entries whose directory is
// gone, while leaving books outside this library alone.
func TestReconcileRegistryRewritesAndDropsEntries(t *testing.T) {
	libraryRoot := t.TempDir()
	root := writeBook(t, libraryRoot, "book-a", "title: \"甲\"\n")
	reg := newFakeRegistry(
		domain.BookSummary{ID: "book-a", RootPath: "/old/device/container/library/novel/book-a", Title: "旧标题"},
		domain.BookSummary{ID: "gone", RootPath: filepath.Join(libraryRoot, "novel", "gone"), Title: "已删除"},
		domain.BookSummary{ID: "external", RootPath: "/somewhere/else/external", Title: "导入的书"},
	)

	if err := ReconcileRegistry(context.Background(), reg, libraryRoot); err != nil {
		t.Fatal(err)
	}

	if got := reg.books["book-a"]; got.RootPath != root || got.Title != "甲" {
		t.Fatalf("book-a=%+v want this device's root and the on-disk title", got)
	}
	if _, ok := reg.books["gone"]; ok {
		t.Fatal("a book deleted from the library must leave the registry")
	}
	if got, ok := reg.books["external"]; !ok || got.RootPath != "/somewhere/else/external" {
		t.Fatalf("external=%+v want an untouched import", got)
	}
}

// `/a/library-x` must not be mistaken for `/a/library`.
func TestReconcileRegistryIgnoresSiblingDirectories(t *testing.T) {
	libraryRoot := t.TempDir()
	reg := newFakeRegistry(domain.BookSummary{
		ID:       "sibling",
		RootPath: libraryRoot + "-other/novel/sibling",
	})
	if err := ReconcileRegistry(context.Background(), reg, libraryRoot); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.books["sibling"]; !ok {
		t.Fatal("a directory outside the library root must not be removed")
	}
}
