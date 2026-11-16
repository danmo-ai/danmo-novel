package books

import (
	"os"
	"path/filepath"
	"testing"
)

func writeShelfBook(t *testing.T, root string) {
	t.Helper()
	mustWrite := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("novel-state.yaml", "title: \"测试书\"\ngenre: \"悬疑\"\nstage: write\nlast_committed_ch: 7\n")
	mustWrite("outline/units/v01-U1.yaml", "unit_id: v01-U1\nstatus: reviewed\ntitle_working: 一\nchapter_range: [1, 3]\n")
	mustWrite("outline/units/v01-U2.yaml", "unit_id: v01-U2\nstatus: accepted\ntitle_working: 二\nchapter_range: [4, 6]\n")
	mustWrite("outline/units/v01-U3.yaml", "unit_id: v01-U3\nstatus: proposed\ntitle_working: 三\nchapter_range: [7, 9]\n")
	mustWrite("units/v01-U1.md", "## 第1章 一\n\n正文。\n")
	mustWrite("canon/cast/lin-an.md", "# 林岸\n")
}

func TestLoadBookSummaryCountsWithoutReadingProse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shelf-book")
	writeShelfBook(t, root)

	summary, err := LoadBookSummary(root, false)
	if err != nil {
		t.Fatalf("LoadBookSummary: %v", err)
	}
	if summary.Title != "测试书" || summary.Genre != "悬疑" {
		t.Fatalf("state fields not read: %+v", summary)
	}
	if summary.UnitCount != 3 {
		t.Fatalf("unit count = %d, want 3", summary.UnitCount)
	}
	// v01-U1 has prose and is reviewed; v01-U2 has no prose so it stays accepted
	// (proposed/accepted are not "drafted"); v01-U3 stays proposed.
	if summary.ReviewedCount != 1 {
		t.Fatalf("reviewed count = %d, want 1", summary.ReviewedCount)
	}
	if summary.DraftedCount != 0 {
		t.Fatalf("drafted count = %d, want 0", summary.DraftedCount)
	}

	// Match the full loader on everything the shelf shows. LoadBook also reads
	// prose, so the numbers must agree even though this path never opened it.
	full, err := LoadBook(root, false)
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	if full.Summary.UnitCount != summary.UnitCount ||
		full.Summary.ReviewedCount != summary.ReviewedCount ||
		full.Summary.DraftedCount != summary.DraftedCount ||
		full.Summary.Title != summary.Title {
		t.Fatalf("summary diverges from full load:\n full=%+v\n shelf=%+v", full.Summary, summary)
	}
}

// A prose file that cannot be read must not change the shelf: presence is a
// stat, and a stat that succeeds while the content is still an iCloud
// placeholder is exactly the case that used to hang the request.
func TestLoadBookSummaryIgnoresUnreadableProse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shelf-book")
	writeShelfBook(t, root)

	prose := filepath.Join(root, "units", "v01-U1.md")
	if err := os.Remove(prose); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(prose, 0o755); err != nil {
		t.Fatal(err)
	}

	summary, err := LoadBookSummary(root, false)
	if err != nil {
		t.Fatalf("LoadBookSummary: %v", err)
	}
	// reviewed came from the outline, so it survives; the directory still
	// "exists", so HasProse is true — content is never needed either way.
	if summary.ReviewedCount != 1 {
		t.Fatalf("reviewed count = %d, want 1", summary.ReviewedCount)
	}
	if summary.UnitCount != 3 {
		t.Fatalf("unit count = %d, want 3", summary.UnitCount)
	}
}

func TestLoadBookSummaryMissingBook(t *testing.T) {
	if _, err := LoadBookSummary(filepath.Join(t.TempDir(), "absent"), false); err == nil {
		t.Fatal("expected an error for a missing book root")
	}
}

func TestLoadBookSummaryFallsBackToBibleHeading(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bible-only")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "book-bible.md"), []byte("# 圣经标题\n\n正文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary, err := LoadBookSummary(root, false)
	if err != nil {
		t.Fatalf("LoadBookSummary: %v", err)
	}
	if summary.Title != "圣经标题" {
		t.Fatalf("title = %q, want 圣经标题", summary.Title)
	}
	if summary.UnitCount != 0 {
		t.Fatalf("unit count = %d, want 0", summary.UnitCount)
	}
}