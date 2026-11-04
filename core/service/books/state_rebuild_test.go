package books

import (
	"os"
	"path/filepath"
	"testing"
)

// 每段都是全量快照：写 U3 时必须看得见 U1 记下的状态，而不是只有 U2 的增量。
func TestRebuildStateFromCarriesFullSnapshots(t *testing.T) {
	root := t.TempDir()
	writeOutline(t, root, "v01-U1", "story_day: 1\nend_day: 4\nstate_deltas:\n  - {stem: lin, field: health, from: \"\", to: \"刀伤\"}\n  - {stem: lin, field: goal, from: \"\", to: \"入组\"}\n")
	writeOutline(t, root, "v01-U2", "story_day: 5\nend_day: 9\nstate_deltas:\n  - {stem: lin, field: knows, from: \"\", to: \"知道甲\"}\n")
	writeOutline(t, root, "v01-U3", "story_day: 10\nend_day: 14\nstate_deltas:\n  - {stem: lin, field: goal, from: \"入组\", to: \"破案\"}\n")

	for _, id := range []string{"v01-U1", "v01-U2", "v01-U3"} {
		if err := RebuildStateFrom(root, id, nil); err != nil {
			t.Fatal(err)
		}
	}
	states, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	last := states[len(states)-1]
	if last.Unit != "v01-U3" {
		t.Fatalf("last=%s", last.Unit)
	}
	snap := last.Cast["lin"]
	if snap.Health != "刀伤" {
		t.Fatalf("U3 必须携带 U1 的 health：%+v", snap)
	}
	if snap.Goal != "破案" {
		t.Fatalf("U3 自己的 delta 必须覆盖：%+v", snap)
	}
	if len(snap.Knows) != 1 || snap.Knows[0] != "知道甲" {
		t.Fatalf("U2 的知情范围必须累积：%+v", snap)
	}
}

// 重写上游单元必须重算下游段：否则下游冻结的是旧上游推导出来的值。
func TestRebuildStateFromCascadesDownstream(t *testing.T) {
	root := t.TempDir()
	writeOutline(t, root, "v01-U1", "story_day: 1\nend_day: 4\nstate_deltas:\n  - {stem: lin, field: goal, from: \"\", to: \"入组\"}\n")
	writeOutline(t, root, "v01-U2", "story_day: 5\nend_day: 9\nstate_deltas:\n  - {stem: lin, field: knows, from: \"\", to: \"知道甲\"}\n")
	writeOutline(t, root, "v01-U3", "story_day: 10\nend_day: 14\nstate_deltas:\n  - {stem: lin, field: knows, from: \"\", to: \"知道乙\"}\n")
	for _, id := range []string{"v01-U1", "v01-U2", "v01-U3"} {
		if err := RebuildStateFrom(root, id, nil); err != nil {
			t.Fatal(err)
		}
	}

	// 重写 U1：goal 改成别的值，U2/U3 完全没动。
	writeOutline(t, root, "v01-U1", "story_day: 1\nend_day: 4\nstate_deltas:\n  - {stem: lin, field: goal, from: \"\", to: \"复仇\"}\n")
	if err := RebuildStateFrom(root, "v01-U1", nil); err != nil {
		t.Fatal(err)
	}
	states, _ := LoadState(root)
	last := states[len(states)-1]
	if last.Cast["lin"].Goal != "复仇" {
		t.Fatalf("下游 U3 必须看到 U1 的新值，实际 %+v", last.Cast["lin"])
	}
	// 下游自己的 delta 仍然生效。
	if len(last.Cast["lin"].Knows) != 2 {
		t.Fatalf("U2/U3 的知情范围不该丢：%+v", last.Cast["lin"])
	}
}

// 从未定稿的单元不能因为级联而不凭空出现状态。
func TestRebuildStateFromStopsAtUnfinalizedUnit(t *testing.T) {
	root := t.TempDir()
	writeOutline(t, root, "v01-U1", "story_day: 1\nstate_deltas:\n  - {stem: lin, field: goal, from: \"\", to: \"入组\"}\n")
	writeOutline(t, root, "v01-U2", "story_day: 5\nstate_deltas:\n  - {stem: lin, field: goal, from: \"\", to: \"破案\"}\n")
	writeOutline(t, root, "v01-U3", "story_day: 9\nstate_deltas:\n  - {stem: lin, field: goal, from: \"\", to: \"升职\"}\n")

	if err := RebuildStateFrom(root, "v01-U1", nil); err != nil {
		t.Fatal(err)
	}
	states, _ := LoadState(root)
	if len(states) != 1 || states[0].Unit != "v01-U1" {
		t.Fatalf("不该凭空为未定稿单元造段：%+v", states)
	}
}

// after_ch 是执行事实，重建不能改掉它。
func TestRebuildStateFromKeepsAfterCh(t *testing.T) {
	root := t.TempDir()
	writeOutline(t, root, "v01-U1", "story_day: 1\nend_day: 4\nstate_deltas:\n  - {stem: lin, field: goal, from: \"\", to: \"入组\"}\n")
	if err := RebuildStateFrom(root, "v01-U1", nil); err != nil {
		t.Fatal(err)
	}
	states, _ := LoadState(root)
	states[0].AfterCh = 2 // 只写完前两章就定稿过
	if err := ReplaceState(root, states); err != nil {
		t.Fatal(err)
	}
	if err := RebuildStateFrom(root, "v01-U1", nil); err != nil {
		t.Fatal(err)
	}
	after, _ := LoadState(root)
	if after[0].AfterCh != 2 {
		t.Fatalf("after_ch 被重建改成了 %d", after[0].AfterCh)
	}
}

func writeOutline(t *testing.T, root, unitID, body string) {
	t.Helper()
	rel := "outline/units/" + unitID + ".yaml"
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("unit_id: "+unitID+"\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}
