package books

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestAcceptPlanVolumeSeedsUnits(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "plan-test", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outline", "book_outline.md"), []byte("# 总纲\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		Mode: domain.PlanModeVolume, VolumeID: "v01",
		PlanMarkdown: map[string]string{
			"volume_outline": "# 第一卷\n\n## 本卷人物\n- lin\n\n## 单元索引\n| unit_id | 章范围 | 一句话功能 | 终局边界 | 下一钩类型 |\n|---|---|---|---|---|\n| v01-U1 | 1-3 | 旧邮袋 | 不揭真相 | 危机 |\n| v01-U2 | 4-6 | 旧识试探 | 不合作 | 身份 |\n",
		},
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Units) != 2 || snap.Units[0].Status != domain.UnitProposed {
		t.Fatalf("units=%+v", snap.Units)
	}
	if _, err := os.Stat(filepath.Join(root, "outline", "volumes", "v01.md")); err != nil {
		t.Fatal(err)
	}
	kind, unit, _ := SelectPrimaryAction(snap)
	if kind != domain.TaskOutline || unit != "v01-U1" {
		t.Fatalf("next=%s %s", kind, unit)
	}
}
