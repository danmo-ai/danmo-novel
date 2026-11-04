package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func unitWithDeltas(id string, deltas ...string) domain.UnitInfo {
	return domain.UnitInfo{UnitID: id, OutlineRaw: "unit_id: " + id + "\nstate_deltas:\n" + strings.Join(deltas, "\n") + "\n"}
}

func delta(stem, field, to string) string {
	return "  - {stem: " + stem + ", field: " + field + ", from: x, to: " + to + "}"
}

// 字段跨过阈值没被刷新时必须被扫出来——这是「账本仍挂着六个单元前的伤势」
// 唯一的自动发现机制。
func TestFindStaleCharacterFields(t *testing.T) {
	units := []domain.UnitInfo{
		unitWithDeltas("v01-U1", delta("lin", "health", "受伤")),
		unitWithDeltas("v01-U2"),
		unitWithDeltas("v01-U3"),
		unitWithDeltas("v01-U4", delta("lin", "goal", "破案")),
	}
	state := domain.UnitState{
		Unit: "v01-U4",
		Cast: map[string]domain.CharacterSnapshot{"lin": {Health: "受伤", Goal: "破案"}},
	}
	got := FindStaleCharacterFields(state, units)
	if len(got) != 1 {
		t.Fatalf("stale=%+v", got)
	}
	if got[0].Field != "health" || got[0].LastUnit != "v01-U1" || got[0].UnitsAgo != 3 {
		t.Fatalf("stale=%+v", got[0])
	}
}

// 阈值内不报：太敏感会让每次审校都刷一堆无意义提示。
func TestFindStaleCharacterFieldsBelowThreshold(t *testing.T) {
	units := []domain.UnitInfo{
		unitWithDeltas("v01-U1", delta("lin", "health", "受伤")),
		unitWithDeltas("v01-U2"),
		unitWithDeltas("v01-U3"),
	}
	state := domain.UnitState{
		Unit: "v01-U3",
		Cast: map[string]domain.CharacterSnapshot{"lin": {Health: "受伤"}},
	}
	if got := FindStaleCharacterFields(state, units); len(got) != 0 {
		t.Fatalf("stale=%+v", got)
	}
}

// 未定稿单元的 delta 不能算作「刷新过」：它们还没发生。
// 第一版没区分，把规划 delta 当成事实，报出一堆假警报。
func TestFindStaleCharacterFieldsIgnoresUnfinalized(t *testing.T) {
	finalized := []domain.UnitInfo{
		unitWithDeltas("v01-U1", delta("lin", "health", "受伤")),
	}
	state := domain.UnitState{
		Unit: "v01-U1",
		Cast: map[string]domain.CharacterSnapshot{"lin": {Health: "受伤"}},
	}
	if got := FindStaleCharacterFields(state, finalized); len(got) != 0 {
		t.Fatalf("刚写入的字段不该报警：%+v", got)
	}
}

// goal / knows 不参与扫描：目标可以长期不变，知情范围只会增加。
func TestFindStaleCharacterFieldsSkipsGoalAndKnows(t *testing.T) {
	units := []domain.UnitInfo{
		domain.UnitInfo{UnitID: "v01-U1", OutlineRaw: "unit_id: v01-U1\nstate_deltas:\n  - {stem: lin, field: goal, from: a, to: b}\n  - {stem: lin, field: knows, from: a, to: 知道甲}\n"},
		domain.UnitInfo{UnitID: "v01-U2"},
		domain.UnitInfo{UnitID: "v01-U3"},
		domain.UnitInfo{UnitID: "v01-U4"},
	}
	state := domain.UnitState{
		Unit: "v01-U4",
		Cast: map[string]domain.CharacterSnapshot{"lin": {Goal: "入组", Knows: []string{"知道甲"}}},
	}
	if got := FindStaleCharacterFields(state, units); len(got) != 0 {
		t.Fatalf("goal/knows 不该报陈旧：%+v", got)
	}
}

// 转成 finding 时必须是 advisory：过期与否要人判断，硬拦会卡住作者。
func TestStaleFieldFindingsAreAdvisory(t *testing.T) {
	fs := StaleFieldFindings(domain.UnitInfo{UnitID: "v01-U4"}, []StaleFieldFinding{{
		Stem: "lin", Field: "health", Value: "受伤", LastUnit: "v01-U1", UnitsAgo: 3,
	}})
	if len(fs) != 1 || fs[0].Severity != "advisory" || fs[0].Lens != "continuity" {
		t.Fatalf("findings=%+v", fs)
	}
	if !strings.Contains(fs[0].Suggestion, "显式终值") {
		t.Fatalf("建议必须指出怎么修：%s", fs[0].Suggestion)
	}
}
