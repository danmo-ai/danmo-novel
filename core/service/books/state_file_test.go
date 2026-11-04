package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestStateAtIsStrictlyBeforeUnit(t *testing.T) {
	states := []domain.UnitState{
		{Unit: "v01-U1", Cast: map[string]domain.CharacterSnapshot{"a": {Location: "山门"}}},
		{Unit: "v01-U2", Cast: map[string]domain.CharacterSnapshot{"a": {Location: "内门"}}},
		{Unit: "v02-U1", Cast: map[string]domain.CharacterSnapshot{"a": {Location: "城镇"}}},
	}

	// 续写 v02-U1：看到 v01-U2 的状态。
	if st, ok := StateAt(states, "v02-U1"); !ok || st.Unit != "v01-U2" {
		t.Fatalf("got %q ok=%v, want v01-U2", st.Unit, ok)
	}
	// 重写 v01-U2：看到 v01-U1 的状态——自己的旧产物不能回流。
	if st, ok := StateAt(states, "v01-U2"); !ok || st.Unit != "v01-U1" {
		t.Fatalf("rewrite saw %q, want v01-U1", st.Unit)
	}
	// 重写第一卷第一个单元：没有前置状态。
	if _, ok := StateAt(states, "v01-U1"); ok {
		t.Fatal("first unit must have no prior state")
	}
	// 尾部新单元（v99）看到最新一段——这是续写正常路径。
	if st, ok := StateAt(states, "v99-U9"); !ok || st.Unit != "v02-U1" {
		t.Fatalf("tail unit should see the newest segment, got %q", st.Unit)
	}
	// 无法解析的单元号不能瞎猜。
	if _, ok := StateAt(states, "draft-unit"); ok {
		t.Fatal("unparseable unit id must not resolve to a snapshot")
	}
}

func TestAppendUnitStateIsIdempotentPerUnit(t *testing.T) {
	root := t.TempDir()
	for _, st := range []domain.UnitState{
		{Unit: "v01-U2", AfterCh: 6},
		{Unit: "v01-U1", AfterCh: 3},
	} {
		if err := AppendUnitState(root, st); err != nil {
			t.Fatal(err)
		}
	}
	// 同一个单元再落一次：覆盖，不追加。
	if err := AppendUnitState(root, domain.UnitState{Unit: "v01-U1", AfterCh: 4}); err != nil {
		t.Fatal(err)
	}
	states, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("segments=%d want 2 (one per unit)", len(states))
	}
	if states[0].Unit != "v01-U1" || states[0].AfterCh != 4 {
		t.Fatalf("first segment=%+v, want overwritten v01-U1 afterCh=4", states[0])
	}
	if states[1].Unit != "v01-U2" {
		t.Fatalf("segments not ordered: %+v", states)
	}
}

func TestLoadStateMissingFileIsNotAnError(t *testing.T) {
	states, err := LoadState(t.TempDir())
	if err != nil {
		t.Fatalf("missing ledger must not fail: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("states=%+v", states)
	}
}

// state_deltas 是状态的唯一来源。旧版的 state_deltas_json 已删除，
// 字段名也不再兼容别名——写错就在细纲接受时报错。
func TestStateFromOutlineReadsBothShapes(t *testing.T) {
	listShape := `unit_id: v01-U1
story_day: 12
state_deltas:
  - {stem: linchen, field: location, from: "山门", to: "内门"}
  - {stem: linchen, field: knows, from: "", to: "知道刀柄的来历"}
`
	st := StateFromOutline(domain.UnitInfo{UnitID: "v01-U1", Volume: 1, OutlineRaw: listShape},
		[]domain.ChapterDraft{{Chapter: 3}})
	if st.AfterCh != 3 || st.StoryDay != 12 {
		t.Fatalf("clock not captured: %+v", st)
	}
	if st.Cast["linchen"].Location != "内门" {
		t.Fatalf("location=%q", st.Cast["linchen"].Location)
	}
	if len(st.Cast["linchen"].Knows) != 1 {
		t.Fatalf("knows=%v", st.Cast["linchen"].Knows)
	}

}

// 没有 state_deltas 的单元不产生空段：空状态比没有状态更误导。
func TestStateFromOutlineSkipsEmptyCast(t *testing.T) {
	st := StateFromOutline(domain.UnitInfo{UnitID: "v01-U1", OutlineRaw: "unit_id: v01-U1\n"}, nil)
	if len(st.Cast) != 0 {
		t.Fatalf("cast=%+v, want empty", st.Cast)
	}
}

func TestMigrateContinuityIsIdempotent(t *testing.T) {
	root, _ := setupFinalizeBook(t)
	first, err := MigrateContinuity(root)
	if err != nil {
		t.Fatal(err)
	}
	if !first.FactsRemoved {
		t.Fatal("legacy facts.md should be removed")
	}
	second, err := MigrateContinuity(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.UnitsRebuilt) != 0 {
		t.Fatalf("second run rebuilt %v, want nothing", second.UnitsRebuilt)
	}
}

// notes 是追加式工作日志，属于 commits；留在 novel-state.yaml 会拖慢解析并误导读者。
func TestMoveStateNotes(t *testing.T) {
	root := t.TempDir()
	state := "book_id: b1\ntitle: t\nnotes: |\n  2026-10-01：第一轮。\n  2026-10-02：第二轮。\n"
	if err := os.WriteFile(filepath.Join(root, "novel-state.yaml"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := moveStateNotes(root)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("notes not moved")
	}
	raw, _ := os.ReadFile(filepath.Join(root, "novel-state.yaml"))
	if strings.Contains(string(raw), "notes:") || strings.Contains(string(raw), "第一轮") {
		t.Fatalf("notes still in state file:\n%s", raw)
	}
	if !strings.Contains(string(raw), "book_id: b1") {
		t.Fatalf("state file lost its scalars:\n%s", raw)
	}
	notes, err := os.ReadFile(filepath.Join(root, "continuity", "commits", "book-notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(notes), "第一轮") || !strings.Contains(string(notes), "第二轮") {
		t.Fatalf("notes content lost:\n%s", notes)
	}
}

func TestSummarizeState(t *testing.T) {
	sum := SummarizeState([]domain.UnitState{
		{Unit: "v01-U1", AfterCh: 3, Cast: map[string]domain.CharacterSnapshot{"a": {}}},
		{Unit: "v01-U2", AfterCh: 6, Cast: map[string]domain.CharacterSnapshot{"a": {}, "b": {}},
			Loops: []domain.LoopEntry{{ID: "F1", Status: "planted"}, {ID: "F2", Status: "paid"}}},
	})
	if sum.Units != 2 || sum.LastUnit != "v01-U2" || sum.AfterCh != 6 || sum.Cast != 2 || sum.OpenLoops != 1 {
		t.Fatalf("summary=%+v", sum)
	}
}
