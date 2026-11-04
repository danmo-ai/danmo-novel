package books

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 回归：backups/ 里的同名单元文件不得覆盖正式 outline/units 文件。
func TestLoaderIgnoresBackupDirs(t *testing.T) {
	root := t.TempDir()
	unitDir := filepath.Join(root, "outline", "units")
	_ = os.MkdirAll(unitDir, 0o755)
	_ = os.WriteFile(filepath.Join(root, "novel-state.yaml"), []byte("title: t\ngenre: g\n"), 0o644)
	good := "unit_id: v01-U1\nstatus: accepted\ntitle_working: \"正确标题\"\nchapter_range: [1, 3]\n"
	_ = os.WriteFile(filepath.Join(unitDir, "v01-U1.yaml"), []byte(good), 0o644)
	// backups/ 与 _archive/ 里的旧版本，文件名同样匹配 unitOutlineRe
	for _, dir := range []string{"backups/pre-repair", "_archive/old", "_archive/legacy-chapters", "backups/pre-accept-1"} {
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

// 单元细纲是 YAML：chapter_range 无论写成行内数组还是块序列都必须读出；
// 行内注释由解析器处理；有正文的单元状态由正文决定，不被细纲覆盖。
func TestLoaderUnitYAML(t *testing.T) {
	root := t.TempDir()
	unitDir := filepath.Join(root, "outline", "units")
	proseDir := filepath.Join(root, "units")
	for _, d := range []string{unitDir, proseDir, filepath.Join(root, "outline"), filepath.Join(root, "outline", "volumes")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := "title: 测试书\ngenre: g\nstage: writing  # 定稿轮备注\nlast_committed_ch: 0\nactive_unit: v01-U2\n"
	_ = os.WriteFile(filepath.Join(root, "novel-state.yaml"), []byte(state), 0o644)
	_ = os.WriteFile(filepath.Join(root, "outline", "book_outline.md"), []byte("# 总纲\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "outline", "volumes", "v01.md"), []byte("# 第一卷\n"), 0o644)
	_ = os.WriteFile(filepath.Join(unitDir, "v01-U1.yaml"), []byte("unit_id: v01-U1\nstatus: accepted\ntitle_working: 甲\nchapter_range:\n    - 1\n    - 3\n"), 0o644)
	_ = os.WriteFile(filepath.Join(unitDir, "v01-U2.yaml"), []byte("unit_id: v01-U2\nstatus: reviewed\ntitle_working: 乙\nchapter_range: [4, 6]\n"), 0o644)
	_ = os.WriteFile(filepath.Join(proseDir, "v01-U1.md"), []byte("## 第1章 甲\n\n正文\n"), 0o644)

	snap, err := LoadBook(root, true)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.UnitInfo{}
	for _, u := range snap.Units {
		byID[u.UnitID] = u
	}
	if u := byID["v01-U1"]; u.ChapterFrom != 1 || u.ChapterTo != 3 {
		t.Fatalf("v01-U1 range=%d-%d, block chapter_range not parsed", u.ChapterFrom, u.ChapterTo)
	}
	if u := byID["v01-U2"]; u.ChapterFrom != 4 || u.ChapterTo != 6 {
		t.Fatalf("v01-U2 range=%d-%d, inline chapter_range not parsed", u.ChapterFrom, u.ChapterTo)
	}
	// accepted + 有正文 = drafted。
	if u := byID["v01-U1"]; u.Status != domain.UnitDrafted {
		t.Fatalf("v01-U1 status=%q, want drafted", u.Status)
	}
	if byID["v01-U2"].Status != domain.UnitReviewed {
		t.Fatalf("v01-U2 status=%q", byID["v01-U2"].Status)
	}
	if u := byID["v01-U1"]; u.Title != "甲" || !u.HasProse {
		t.Fatalf("v01-U1 title=%q prose=%v", u.Title, u.HasProse)
	}
}

// 回归：卷纲一次把整卷单元播成 proposed，primary 不能让后面的细纲插队，
// 否则前面 accepted 的单元永远等不到「写正文」。
func TestPrimaryActionFollowsUnitOrder(t *testing.T) {
	snap := domain.BookSnapshot{
		Volumes: []domain.VolumeInfo{{Volume: 1, FileName: "v01.md"}},
		Units: []domain.UnitInfo{
			{UnitID: "v01-U1", Volume: 1, Index: 1, Status: domain.UnitFinalized},
			{UnitID: "v01-U2", Volume: 1, Index: 2, Status: domain.UnitAccepted},
			{UnitID: "v01-U3", Volume: 1, Index: 3, Status: domain.UnitProposed},
			{UnitID: "v01-U4", Volume: 1, Index: 4, Status: domain.UnitProposed},
		},
		Files: map[string]string{"outline/book_outline.md": "# 总纲\n"},
	}
	kind, unit, vol := SelectPrimaryAction(snap)
	if kind != domain.TaskWrite || unit != "v01-U2" || vol != 1 {
		t.Fatalf("primary=%s %s vol=%d, want write v01-U2 vol=1", kind, unit, vol)
	}
	snap.Units[1].Status = domain.UnitDrafted
	if kind, unit, _ := SelectPrimaryAction(snap); kind != domain.TaskReview || unit != "v01-U2" {
		t.Fatalf("draft primary=%s %s, want review v01-U2", kind, unit)
	}
	for i := range snap.Units {
		snap.Units[i].Status = domain.UnitReviewed
	}
	if _, _, vol := SelectPrimaryAction(snap); vol != 2 {
		t.Fatalf("all reviewed must advance to next volume, got vol=%d", vol)
	}
}

// 回归：active_unit 停在已完工的卷（作者定稿完一卷忘了改）时，不能直接跳到
// 「立项下一卷」——下一卷里已经种好、只差细纲的单元才是真正的下一步。
func TestPrimaryActionStaleActiveUnit(t *testing.T) {
	snap := domain.BookSnapshot{
		StateRaw: "title: t\nactive_unit: v07-U2\n",
		Volumes: []domain.VolumeInfo{
			{Volume: 7, FileName: "v07.md"},
			{Volume: 8, FileName: "v08.md"},
		},
		Units: []domain.UnitInfo{
			{UnitID: "v07-U1", Volume: 7, Index: 1, Status: domain.UnitFinalized},
			{UnitID: "v07-U2", Volume: 7, Index: 2, Status: domain.UnitFinalized},
			{UnitID: "v08-U4", Volume: 8, Index: 4, Status: domain.UnitFinalized},
			{UnitID: "v08-U5", Volume: 8, Index: 5, Status: domain.UnitProposed},
			{UnitID: "v08-U6", Volume: 8, Index: 6, Status: domain.UnitProposed},
		},
		Files: map[string]string{"outline/book_outline.md": "# 总纲\n"},
	}
	kind, unit, vol := SelectPrimaryAction(snap)
	if kind != domain.TaskOutline || unit != "v08-U5" || vol != 8 {
		t.Fatalf("primary=%s %s vol=%d, want outline v08-U5 vol=8", kind, unit, vol)
	}
	// active_unit 所属卷仍有待办时，仍然优先它（而不是更早的空卷）。
	snap.StateRaw = "title: t\nactive_unit: v08-U5\n"
	snap.Units = append(snap.Units, domain.UnitInfo{UnitID: "v07-U3", Volume: 7, Index: 3, Status: domain.UnitAccepted})
	if kind, unit, vol := SelectPrimaryAction(snap); kind != domain.TaskOutline || unit != "v08-U5" || vol != 8 {
		t.Fatalf("with active volume unfinished: primary=%s %s vol=%d", kind, unit, vol)
	}
	// 全书都定稿了才立项下一卷。
	for i := range snap.Units {
		snap.Units[i].Status = domain.UnitReviewed
	}
	if kind, _, vol := SelectPrimaryAction(snap); kind != domain.TaskPlan || vol != 9 {
		t.Fatalf("all reviewed: primary=%s vol=%d, want plan vol=9", kind, vol)
	}
}

func TestParseVolumeCast(t *testing.T) {
	md := "# 卷纲\n\n## 本卷人物（stem；批准即 canon）\n\n- `lu-chen`\n- `kang-yongnian`\n- 陆沉（主角）\n- wen-nianci 备注文字\n- 01-lu-chen\n"
	got := parseVolumeCast(md)
	want := []string{"lu-chen", "kang-yongnian", "wen-nianci"}
	if len(got) != len(want) {
		t.Fatalf("parseVolumeCast=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseVolumeCast=%v, want %v", got, want)
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
