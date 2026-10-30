package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func sampleSettings() map[string]string {
	out := map[string]string{}
	for _, p := range PlanSettingsPaths {
		out[p] = "# " + p + "\ncontent\n"
	}
	out["canon/locked-terms.yaml"] = "locked_until: {}\ncompliance: []\naliases: {}\n"
	return out
}

func sampleVolumeOutline() string {
	return `# 第一卷

## 本卷人物
- lin

## 单元索引
| unit_id | 章范围 | 一句话功能 | 终局边界 | 下一钩类型 |
|---|---|---|---|---|
| v01-U1 | 1-3 | 旧邮袋 | 不揭真相 | 危机 |
| v01-U2 | 4-6 | 旧识试探 | 不合作 | 身份 |
`
}

func TestAcceptPlanInitWritesSettingsAndCastOnly(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "init-test", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		Mode: domain.PlanModeInit,
		PlanMarkdown: map[string]string{
			"book_outline": "# 总纲\n",
			"cast_json":    `[{"stem":"lin","name":"林晚","role":"protagonist","tagline":"痕迹检验","cardMarkdown":"# 林晚\n"}]`,
		},
		PlanSettings: sampleSettings(),
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	for _, p := range PlanSettingsPaths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
			t.Fatalf("missing setting %s: %v", p, err)
		}
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !HasBookOutline(snap) {
		t.Fatal("expected book outline")
	}
	if len(snap.Volumes) != 0 {
		t.Fatalf("init must not write volumes: %d", len(snap.Volumes))
	}
	if len(snap.Units) != 0 {
		t.Fatalf("init must not seed units: %d", len(snap.Units))
	}
	if len(snap.Cast) != 1 || snap.Cast[0].Status != domain.CastCanon {
		t.Fatalf("cast=%+v", snap.Cast)
	}
	kind, _, vol := SelectPrimaryAction(snap)
	if kind != domain.TaskPlan || vol != 1 {
		t.Fatalf("next after init should be volume plan, got %s vol=%d", kind, vol)
	}
}

func TestAcceptPlanReinitPreservesVolumeUnitsProse(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "reinit-test", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	volPath := filepath.Join(root, "outline", "volumes", "v01.md")
	if err := os.MkdirAll(filepath.Dir(volPath), 0o755); err != nil {
		t.Fatal(err)
	}
	volBody := "# 保留卷纲\n"
	if err := os.WriteFile(volPath, []byte(volBody), 0o644); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(root, "outline", "units", "v01-U1.yaml")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "unit_id: v01-U1\nstatus: drafted\ndesire: keep-me\n"
	if err := os.WriteFile(unitPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	prosePath := filepath.Join(root, "units", "v01-U1.md")
	if err := os.MkdirAll(filepath.Dir(prosePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prosePath, []byte("正文保留\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	factsPath := filepath.Join(root, "continuity", "facts.md")
	if err := os.MkdirAll(filepath.Dir(factsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(factsPath, []byte("# facts keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cand := domain.Candidate{
		Mode: domain.PlanModeReinit,
		PlanMarkdown: map[string]string{
			"book_outline": "# 新总纲\n",
			"cast_json":    `[{"stem":"lin","name":"林晚","role":"protagonist","cardMarkdown":"# 林晚\n"}]`,
		},
		PlanSettings: sampleSettings(),
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("existing unit overwritten:\n%s", got)
	}
	prose, err := os.ReadFile(prosePath)
	if err != nil || !strings.Contains(string(prose), "正文保留") {
		t.Fatalf("prose touched: %s %v", prose, err)
	}
	vol, err := os.ReadFile(volPath)
	if err != nil || string(vol) != volBody {
		t.Fatalf("volume touched: %s %v", vol, err)
	}
	facts, err := os.ReadFile(factsPath)
	if err != nil || !strings.Contains(string(facts), "facts keep") {
		t.Fatalf("continuity touched: %s %v", facts, err)
	}
	if _, err := os.Stat(filepath.Join(root, "outline", "units", "v01-U2.yaml")); err == nil {
		t.Fatal("reinit must not seed units")
	}
	bible, _ := os.ReadFile(filepath.Join(root, "book-bible.md"))
	if !strings.Contains(string(bible), "book-bible.md") {
		t.Fatalf("settings not rewritten: %s", bible)
	}
}

func TestAcceptPlanReinitPreservePathsKeepsFiles(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "reinit-preserve", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "canon", "cast"), 0o755); err != nil {
		t.Fatal(err)
	}
	castPath := filepath.Join(root, "canon", "cast", "lin.md")
	refined := "# 林晚\n`status`: canon\n`role`: protagonist\n精修版\n"
	if err := os.WriteFile(castPath, []byte(refined), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outline", "book_outline.md"), []byte("# 旧总纲\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		Mode:          domain.PlanModeReinit,
		PreservePaths: []string{"canon/cast/lin.md", "canon/world.md"},
		PlanMarkdown: map[string]string{
			"book_outline": "# 新总纲\n",
			"cast_json":    `[{"stem":"lin","name":"林晚","role":"protagonist","cardMarkdown":"# 林晚 覆盖版\n"}]`,
		},
		PlanSettings: sampleSettings(),
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(castPath)
	if string(got) != refined {
		t.Fatalf("preserved cast overwritten: %s", got)
	}
	world, _ := os.ReadFile(filepath.Join(root, "canon", "world.md"))
	if strings.Contains(string(world), "# canon/world.md\ncontent") {
		t.Fatalf("preserved world overwritten: %s", world)
	}
	book, _ := os.ReadFile(filepath.Join(root, "outline", "book_outline.md"))
	if !strings.Contains(string(book), "新总纲") {
		t.Fatalf("book outline should be replaced: %s", book)
	}
}

func TestAcceptPlanVolumeDoesNotWriteSettingsCastOrBook(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "vol-test", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outline", "book_outline.md"), []byte("# 既有总纲\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "book-bible.md"))
	if err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		Mode: domain.PlanModeVolume, VolumeID: "v01",
		PlanMarkdown: map[string]string{
			"book_outline":   "# 应忽略的总纲\n",
			"volume_outline": sampleVolumeOutline(),
			"cast_json":      `[{"stem":"lin","name":"林晚","role":"protagonist","cardMarkdown":"# 林晚\n"}]`,
		},
		PlanSettings: map[string]string{"book-bible.md": "# hacked\n"},
	}
	if err := AcceptPlan(root, cand); err == nil {
		t.Fatal("expected reject when volume mode carries settings")
	}
	cand.PlanSettings = nil
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, "book-bible.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("volume mode must not rewrite settings")
	}
	raw, _ := os.ReadFile(filepath.Join(root, "outline", "book_outline.md"))
	if strings.Contains(string(raw), "应忽略的总纲") {
		t.Fatal("volume mode must not write book outline")
	}
	if _, err := os.Stat(filepath.Join(root, "canon", "cast", "lin.md")); err == nil {
		t.Fatal("volume mode must not write cast")
	}
	if _, err := os.Stat(filepath.Join(root, "outline", "volumes", "v01.md")); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Units) != 2 {
		t.Fatalf("units=%d", len(snap.Units))
	}
}
