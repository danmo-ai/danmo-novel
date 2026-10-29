package books

import (
	"os"
	"path/filepath"
	"testing"
)

// 回归：backups/ 里的同名单元文件不得覆盖正式 outline/units 文件。
func TestLoaderIgnoresBackupDirs(t *testing.T) {
	root := t.TempDir()
	unitDir := filepath.Join(root, "outline", "units")
	_ = os.MkdirAll(unitDir, 0o755)
	_ = os.WriteFile(filepath.Join(root, "novel-state.yaml"), []byte("title: t\ngenre: g\n"), 0o644)
	good := "unit_id: v01-U1\nstatus: accepted\ntitle_working: \"正确标题\"\nchapter_range: [1, 3]\n"
	_ = os.WriteFile(filepath.Join(unitDir, "v01-U1.yaml"), []byte(good), 0o644)
	// backups 与 .tools 里的旧版本，文件名同样匹配 unitOutlineRe
	for _, dir := range []string{"backups/pre-repair", ".tools/pack", "tools/x", "extras/old"} {
		d := filepath.Join(root, dir)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "v01-U1.yaml"), []byte("unit_id: v01-U1\nstatus: proposed\ntitle_working: \"错误旧标题\"\n"), 0o644)
	}
	for i := 0; i < 20; i++ {
		snap, err := LoadBook(root, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Units) != 1 {
			t.Fatalf("units=%d", len(snap.Units))
		}
		if snap.Units[0].Title != "正确标题" {
			t.Fatalf("round %d: backup overwrote unit: %s", i, snap.Units[0].Title)
		}
	}
}

func TestSplitNameTagline(t *testing.T) {
	cases := []struct{ in, name, tagline string }{
		{"# 陆沉（主角）", "陆沉", ""},
		{"# 白秉修（白家第一代）", "白秉修", "白家第一代"},
		{"# 白亦修（终极反派）", "白亦修", "终极反派"},
		{"# 柴茂林（省城运钞车案主犯 · 前银行押运队司机）", "柴茂林", "省城运钞车案主犯 · 前银行押运队司机"},
		{"# 林晚", "林晚", ""},
		{"# 林尘 (protagonist)", "林尘", ""},
		{"# 顾长山（师兄）", "顾长山", "师兄"},
	}
	for _, c := range cases {
		n, tg := splitNameTagline(c.in)
		if n != c.name || tg != c.tagline {
			t.Fatalf("splitNameTagline(%q) = (%q, %q), want (%q, %q)", c.in, n, tg, c.name, c.tagline)
		}
	}
}

func TestLoadSampleBook(t *testing.T) {
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	snap, err := LoadBook(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Summary.Title != "试炼长生" {
		t.Fatalf("title=%q", snap.Summary.Title)
	}
	if len(snap.Units) != 1 || snap.Units[0].Status != "accepted" {
		t.Fatalf("units=%+v", snap.Units)
	}
	if len(snap.Cast) != 1 || snap.Cast[0].Role != "protagonist" {
		t.Fatalf("cast=%+v", snap.Cast)
	}
	kind, unit, _ := SelectPrimaryAction(snap)
	if kind != "write" || unit != "v01-U1" {
		t.Fatalf("primary=%s %s", kind, unit)
	}
	if snap.Revision == "" {
		t.Fatal("empty revision")
	}
}
