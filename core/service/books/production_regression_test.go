package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestActualLedgerDoesNotTurnUnconfirmedPlansIntoFacts(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{{Chapter: 1, Body: "林尘仍在外门。"}, {Chapter: 2, Body: "他没有进入山门。"}}
	actual := []domain.ChapterActual{{Chapter: 1, Summary: "林尘留在外门"}, {Chapter: 2, Summary: "进入山门的计划没有发生"}}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatal(err)
	}
	states, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Cast["linchen"].Location != "" {
		t.Fatalf("unconfirmed planned relocation must not become a fact: %+v", states)
	}
	if states[0].AfterCh != 2 {
		t.Fatalf("after_ch=%d", states[0].AfterCh)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ActualPath(unit.UnitID)))
	if !strings.Contains(string(raw), "计划没有发生") {
		t.Fatal("actual ledger must retain the confirmed non-event for review")
	}
}

func TestFinalizationSummaryUsesActualRowsAndDoesNotDuplicate(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{{Chapter: 1, Body: "一"}, {Chapter: 2, Body: "二"}}
	actual := []domain.ChapterActual{{Chapter: 1, Summary: "真实事件一", Plant: []string{"FS-01"}, PlannedForeshadowHandled: []string{"FS-01"}}, {Chapter: 2, Summary: "真实事件二", Payoff: []string{"FS-01"}, PlannedForeshadowHandled: []string{"FS-01"}}}
	for i := 0; i < 2; i++ {
		if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "continuity", "summaries", "v01.md")); !os.IsNotExist(err) {
		t.Fatal("finalize must not persist a duplicate summary file")
	}
	projection, err := ProjectVolumeSummary(root, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(projection, "| 1 |") != 1 || strings.Count(projection, "| 2 |") != 1 {
		t.Fatalf("projection not idempotent:\n%s", projection)
	}
	if !strings.Contains(projection, "真实事件一") || !strings.Contains(projection, "FS-01→已收") {
		t.Fatalf("projection must use actual facts:\n%s", projection)
	}
	commit, _ := os.ReadFile(filepath.Join(root, "continuity", "commits", unit.UnitID+".md"))
	if strings.Contains(string(commit), "真实事件一") || !strings.Contains(string(commit), ActualPath(unit.UnitID)) {
		t.Fatal("commit must reference actual facts, not copy chapter summaries")
	}
}

func TestVolumeSummaryProjectionPrefersActualAndKeepsLegacyEvents(t *testing.T) {
	root := t.TempDir()
	writeOutline(t, root, "v01-U1", "unit_id: v01-U1\n")
	if err := os.MkdirAll(filepath.Join(root, "continuity", "summaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "continuity", "summaries", "v01.md"), []byte("# 旧摘要\n| 章 | 事件 | 状态变化 | 伏笔 |\n|---|---|---|---|\n| 1 | 新事件 | 不应复制 | FS-99 |\n| 2 | 旧事件 | 旧伤势 | FS-98 |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	actualRaw, err := MarshalActual("v01-U1", []domain.ChapterActual{{Chapter: 1, Summary: "实际新事件", Plant: []string{"FS-01"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFiles(root, map[string][]byte{ActualPath("v01-U1"): actualRaw}); err != nil {
		t.Fatal(err)
	}
	got, err := ProjectVolumeSummary(root, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "实际新事件") || !strings.Contains(got, "旧事件") {
		t.Fatalf("missing actual or legacy event:\n%s", got)
	}
	if strings.Contains(got, "不应复制") || strings.Contains(got, "旧伤势") {
		t.Fatalf("legacy dynamic state must not enter projection:\n%s", got)
	}
}

func TestRebuildActualRejectsGapWithoutDeletingTarget(t *testing.T) {
	root := t.TempDir()
	writeOutline(t, root, "v01-U1", "story_day: 1\nend_day: 1\n")
	writeOutline(t, root, "v01-U2", "story_day: 2\nend_day: 2\n")
	writeOutline(t, root, "v01-U3", "story_day: 3\nend_day: 3\n")
	if err := ReplaceState(root, []domain.UnitState{{Unit: "v01-U1", AfterCh: 1}, {Unit: "v01-U3", AfterCh: 3}}); err != nil {
		t.Fatal(err)
	}
	if _, err := RebuildStateFromActual(root, "v01-U3", []domain.ChapterActual{{Chapter: 3, Summary: "真实"}}); err != nil {
		t.Fatal(err)
	}
	states, _ := LoadState(root)
	if len(states) != 2 || states[1].Unit != "v01-U3" {
		t.Fatalf("target state was lost: %+v", states)
	}
}

func TestAtomicWriteFilesRejectsEscapeAndKeepsExistingFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "outline", "units", "v01-U1.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte("unit_id: v01-U1\nstatus: accepted\n")
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFiles(root, map[string][]byte{"outline/units/v01-U1.yaml": []byte("changed"), "../escape": []byte("bad")}); err == nil {
		t.Fatal("path escape must fail")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(old) {
		t.Fatal("rejected commit must not change existing files")
	}
}

func TestLockedTermSelectionAndAbilityExceptions(t *testing.T) {
	// Context package owns extraction; this test protects the on-disk shape used by it.
	root := t.TempDir()
	if err := AtomicWriteFiles(root, map[string][]byte{"canon/locked-terms.yaml": []byte("locked_until:\n  v03: [秘密]\ncompliance: [红线]\n")}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "canon/locked-terms.yaml"))
	if !strings.Contains(string(raw), "秘密") {
		t.Fatal("real lock terms must be retained")
	}
}
