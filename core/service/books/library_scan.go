package books

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// LibraryRegistry is the SQLite surface needed to reconcile the on-disk book
// library with the registry the API reads. The store implements it.
type LibraryRegistry interface {
	ListBooks(ctx context.Context) ([]domain.BookSummary, error)
	UpsertBook(ctx context.Context, id, root, title, revision string, readOnly bool) error
	DeleteBook(ctx context.Context, id string) error
}

// ScannedBook is one book directory found under <libraryRoot>/novel.
type ScannedBook struct {
	ID    string
	Root  string
	Title string
	Genre string
	Stage string
}

// ScanLibrary lists book directories in a library root without loading them.
//
// The registry cannot be trusted as the source of truth once the library lives
// in iCloud: another device can add or delete a directory while this one is
// asleep, and the absolute container path differs per device. So startup always
// rescans and reconciles instead of trusting stored roots.
//
// A directory counts as a book only when it holds novel-state.yaml or
// book-bible.md at its top level; `backups/`, `_archive/` and iCloud conflict
// copies are ignored because they never carry both markers.
func ScanLibrary(libraryRoot string) ([]ScannedBook, error) {
	novelRoot := filepath.Join(libraryRoot, "novel")
	entries, err := os.ReadDir(novelRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]ScannedBook, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		root := filepath.Join(novelRoot, name)
		book, ok := scanBookDir(root)
		if !ok {
			continue
		}
		out = append(out, book)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func scanBookDir(root string) (ScannedBook, bool) {
	stateRaw, stateErr := os.ReadFile(filepath.Join(root, "novel-state.yaml"))
	bibleRaw, bibleErr := os.ReadFile(filepath.Join(root, "book-bible.md"))
	if stateErr != nil && bibleErr != nil {
		return ScannedBook{}, false
	}
	state := parseState(string(stateRaw))
	id := filepath.Base(root)
	title := state.Title
	if strings.TrimSpace(title) == "" {
		title = firstMarkdownHeading(string(bibleRaw))
	}
	if strings.TrimSpace(title) == "" {
		title = id
	}
	return ScannedBook{
		ID:    id,
		Root:  root,
		Title: title,
		Genre: state.Genre,
		Stage: state.Stage,
	}, true
}

func firstMarkdownHeading(md string) string {
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

// ReconcileRegistry rewrites the registry so it matches the books on disk.
//
// Books under the library root that disappeared are removed; discovered
// directories are upserted with this device's absolute path. Entries outside
// the library root are left alone — they belong to an explicit import and this
// function has no authority over them.
func ReconcileRegistry(ctx context.Context, reg LibraryRegistry, libraryRoot string) error {
	scanned, err := ScanLibrary(libraryRoot)
	if err != nil {
		return err
	}
	existing, err := reg.ListBooks(ctx)
	if err != nil {
		return err
	}
	existingByID := make(map[string]domain.BookSummary, len(existing))
	for _, book := range existing {
		existingByID[book.ID] = book
	}
	onDisk := make(map[string]bool, len(scanned))
	for _, book := range scanned {
		onDisk[book.ID] = true
		// Skipping an unchanged row keeps imported_at (and therefore the shelf
		// order) stable across launches; only a move or rename needs a write.
		if prev, ok := existingByID[book.ID]; ok &&
			filepath.Clean(prev.RootPath) == filepath.Clean(book.Root) &&
			prev.Title == book.Title {
			continue
		}
		if err := reg.UpsertBook(ctx, book.ID, book.Root, book.Title, "", false); err != nil {
			return fmt.Errorf("register book %s: %w", book.ID, err)
		}
	}
	for _, book := range existing {
		if onDisk[book.ID] || !isUnderLibrary(book.RootPath, libraryRoot) {
			continue
		}
		if err := reg.DeleteBook(ctx, book.ID); err != nil {
			return fmt.Errorf("unregister book %s: %w", book.ID, err)
		}
	}
	return nil
}

// isUnderLibrary reports whether root is a book directory inside libraryRoot.
// It compares path segments, so `/a/library-x` is not treated as `/a/library`.
func isUnderLibrary(root, libraryRoot string) bool {
	root = filepath.Clean(root)
	novelRoot := filepath.Clean(filepath.Join(libraryRoot, "novel"))
	rel, err := filepath.Rel(novelRoot, root)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !strings.ContainsRune(rel, filepath.Separator)
}
