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
	seedTestIdentity(t, root)
	seedForeshadowSchedule(t, root)
	if err := os.WriteFile(filepath.Join(root, "outline", "book_outline.md"), []byte(sampleBookOutline()), 0o644); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		Mode: domain.PlanModeVolume, VolumeID: "v01",
		PlanMarkdown: map[string]string{
			"volume_outline": sampleVolumeOutline(),
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
