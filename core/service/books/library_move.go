package books

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/service/knowledge"
)

// LibraryMoveReport is what MoveLibrary actually relocated.
type LibraryMoveReport struct {
	Books     int `json:"books"`
	Knowledge int `json:"knowledge"`
}

// LibraryConflicts lists book ids that already exist at the target root.
//
// The caller checks this before starting a move: silently merging two books with
// one id would pick one winner per file and leave the other's chapters behind.
func LibraryConflicts(fromRoot, toRoot string) ([]string, error) {
	fromRoot = filepath.Clean(fromRoot)
	toRoot = filepath.Clean(toRoot)
	if !filepath.IsAbs(toRoot) {
		return nil, fmt.Errorf("library directory must be an absolute path: %s", toRoot)
	}
	if fromRoot == toRoot {
		return nil, nil
	}
	if isWithin(fromRoot, toRoot) || isWithin(toRoot, fromRoot) {
		return nil, fmt.Errorf("library directory cannot be inside another library: %s", toRoot)
	}
	existing, err := ScanLibrary(fromRoot)
	if err != nil {
		return nil, err
	}
	var conflicts []string
	for _, book := range existing {
		if _, err := os.Stat(filepath.Join(toRoot, "novel", book.ID)); err == nil {
			conflicts = append(conflicts, book.ID)
		}
	}
	return conflicts, nil
}

// MoveLibrary relocates a whole library root: every book directory and the
// author's knowledge articles.
//
// One implementation serves both entry points — the author picking a new
// directory, and the first macOS launch that finds iCloud where the books used
// to be local. Entries already present at the target are left alone, so an
// interrupted move is finished by running it again instead of producing
// duplicates. Callers that must not merge two books with one id pre-check with
// LibraryConflicts; this function only guarantees "don't overwrite".
func MoveLibrary(fromRoot, toRoot string) (LibraryMoveReport, error) {
	var report LibraryMoveReport
	fromRoot = filepath.Clean(fromRoot)
	toRoot = filepath.Clean(toRoot)
	if fromRoot == toRoot {
		return report, nil
	}
	if !filepath.IsAbs(toRoot) {
		return report, fmt.Errorf("library directory must be an absolute path: %s", toRoot)
	}
	if isWithin(fromRoot, toRoot) || isWithin(toRoot, fromRoot) {
		return report, fmt.Errorf("library directory cannot be inside another library: %s", toRoot)
	}

	books, err := ScanLibrary(fromRoot)
	if err != nil {
		return report, err
	}
	for _, book := range books {
		dst := filepath.Join(toRoot, "novel", book.ID)
		if err := movePath(book.Root, dst); err != nil {
			return report, fmt.Errorf("move book %s: %w", book.ID, err)
		}
		report.Books++
	}
	// Knowledge articles are the author's own text too, and they live under the
	// library root, so a directory change has to take them along or the edited
	// versions silently revert to the built-ins.
	knowledgeMoved, err := moveKnowledge(fromRoot, toRoot)
	if err != nil {
		return report, err
	}
	report.Knowledge = knowledgeMoved

	removeIfEmpty(filepath.Join(fromRoot, "novel"))
	removeIfEmpty(knowledge.Root(fromRoot))
	return report, nil
}

func moveKnowledge(fromRoot, toRoot string) (int, error) {
	entries, err := os.ReadDir(knowledge.Root(fromRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	moved := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		src := filepath.Join(knowledge.Root(fromRoot), entry.Name())
		dst := filepath.Join(knowledge.Root(toRoot), entry.Name())
		if err := movePath(src, dst); err != nil {
			return moved, fmt.Errorf("move knowledge %s: %w", entry.Name(), err)
		}
		moved++
	}
	return moved, nil
}

// movePath renames src to dst, falling back to copy+delete when the two are on
// different volumes. An existing dst is never overwritten.
func movePath(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return nil // already at the target: a retried move, not a conflict
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-volume (or a filesystem that refuses the rename): copy, then drop
	// the source only after the copy succeeded.
	if err := copyPath(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// copyPath copies a file or directory tree. iCloud materialization replaces
// files with placeholders, so every read goes through os.Open/io.Copy — nothing
// here assumes a file is already resident.
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		_ = in.Close()
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	_ = in.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

// isWithin reports whether child sits inside parent.
func isWithin(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
