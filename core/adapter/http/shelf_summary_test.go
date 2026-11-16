package httpapi

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// A book whose metadata read blocks must not hold the shelf: past the deadline
// shelfSummary reports failure so the caller can fall back to the registry row.
// A FIFO reproduces the "read never returns" behaviour of an undownloaded
// iCloud placeholder without depending on iCloud.
func TestShelfSummaryTimesOutOnBlockedRead(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blocked-book")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// novel-state.yaml is the first file LoadBookSummary reads.
	fifo := filepath.Join(root, "novel-state.yaml")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	start := time.Now()
	_, ok := shelfSummary(domain.BookSummary{ID: "blocked-book", RootPath: root}, 150*time.Millisecond)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("expected the blocked book to be reported as unreadable")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("shelf waited %v on one blocked book; the deadline was 150ms", elapsed)
	}
}

func TestShelfSummaryReturnsRealCounts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "normal-book")
	if err := os.MkdirAll(filepath.Join(root, "outline", "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "novel-state.yaml"), []byte("title: \"正常书\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outline", "units", "v01-U1.yaml"),
		[]byte("unit_id: v01-U1\nstatus: reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, ok := shelfSummary(domain.BookSummary{ID: "normal-book", RootPath: root}, 3*time.Second)
	if !ok {
		t.Fatal("expected the readable book to load")
	}
	if summary.Title != "正常书" || summary.UnitCount != 1 || summary.ReviewedCount != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}