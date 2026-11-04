package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 词表之外的名字必须报错，而不是落盘后被 switch 静默丢弃。
// 这是过去 70% 写入消失（399 条里 281 条）的根因。
func TestValidateStateDeltasRejectsUnknownField(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U2", Volume: 1}
	raw := "state_deltas:\n  - {stem: lin, field: method, from: a, to: b}\n"
	err := ValidateStateDeltas(nil, unit, raw, domain.UnitState{}, nil)
	if err == nil || !strings.Contains(err.Error(), "method") {
		t.Fatalf("未识别的 field 必须报错：%v", err)
	}
}

func TestValidateStateDeltasAcceptsAllDeclaredFields(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U2", Volume: 1}
	raw := "state_deltas:\n" +
		"  - {stem: lin, field: location, from: a, to: b}\n" +
		"  - {stem: lin, field: health, from: 受伤, to: 已愈}\n" +
		"  - {stem: career, field: resources, from: a, to: b}\n" +
		"  - {stem: lin, field: knows, from: a, to: 知道钥匙}\n"
	if err := ValidateStateDeltas(nil, unit, raw, domain.UnitState{}, nil); err != nil {
		t.Fatalf("词表内的字段不该被拦：%v", err)
	}
}

// 不做别名兼容：旧写法由迁移改名，运行期写 injury 就是错。
func TestValidateStateDeltasRejectsAliases(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U2", Volume: 1}
	raw := "state_deltas:\n  - {stem: lin, field: injury, from: a, to: b}\n"
	err := ValidateStateDeltas(nil, unit, raw, domain.UnitState{}, nil)
	if err == nil || !strings.Contains(err.Error(), "injury") {
		t.Fatalf("injury 必须被当作非法字段：%v", err)
	}
}

// 生命状态单向：死者不能复活。
func TestValidateStateDeltasRejectsLifeRegression(t *testing.T) {
	cast := []domain.CastCard{{Stem: "lin", Name: "林晚", Role: "recurring", Status: domain.CastCanon}}
	prior := domain.UnitState{Cast: map[string]domain.CharacterSnapshot{"lin": {Life: "dead"}}}
	raw := "state_deltas:\n  - {stem: lin, field: life, from: dead, to: alive}\n"
	unit := domain.UnitInfo{UnitID: "v01-U3", Volume: 1}
	err := ValidateStateDeltas(cast, unit, raw, prior, nil)
	if err == nil || !strings.Contains(err.Error(), "单向") {
		t.Fatalf("死者复活必须被拦：%v", err)
	}
}

// 已死亡的角色不得再进 onStage——账本此前没有死亡记录，所以这条从没生效过。
func TestValidateStateDeltasRejectsDeadOnStage(t *testing.T) {
	prior := domain.UnitState{Cast: map[string]domain.CharacterSnapshot{"lin": {Life: "dead"}}}
	unit := domain.UnitInfo{UnitID: "v01-U3", Volume: 1}
	err := ValidateStateDeltas(nil, unit, "", prior, []string{"lin"})
	if err == nil || !strings.Contains(err.Error(), "已死亡") {
		t.Fatalf("死者进 onStage 必须被拦：%v", err)
	}
}

// role_title / life 只对真实人物有意义；career 这类非人物实体不能改身份。
func TestValidateStateDeltasRejectsRoleTitleForNonCast(t *testing.T) {
	cast := []domain.CastCard{{Stem: "lin", Name: "林晚", Role: "recurring", Status: domain.CastCanon}}
	raw := "state_deltas:\n  - {stem: career, field: role_title, from: a, to: b}\n"
	unit := domain.UnitInfo{UnitID: "v01-U2", Volume: 1}
	err := ValidateStateDeltas(cast, unit, raw, domain.UnitState{}, nil)
	if err == nil || !strings.Contains(err.Error(), "人物卡中没有") {
		t.Fatalf("非人物实体改 role_title 必须被拦：%v", err)
	}
}

// 定稿写全量快照：上一段的状态与人物不能因为本单元没提就消失。
func TestStateFromOutlineWithBaseCarriesPriorState(t *testing.T) {
	base := domain.UnitState{
		Unit: "v01-U1",
		Cast: map[string]domain.CharacterSnapshot{
			"lin":    {Health: "刀伤未愈", Goal: "争取入组", Knows: []string{"知道钥匙"}},
			"career": {Resources: "协查权限"},
		},
	}
	raw := "unit_id: v01-U2\nstory_day: 12\nstate_deltas:\n  - {stem: lin, field: health, from: 刀伤未愈, to: 已愈}\n"
	st := StateFromOutlineWithBase(base, domain.UnitInfo{UnitID: "v01-U2", Volume: 1, OutlineRaw: raw}, nil)

	if st.Cast["lin"].Health != "已愈" {
		t.Fatalf("本单元变化未生效：%+v", st.Cast["lin"])
	}
	if st.Cast["lin"].Goal != "争取入组" {
		t.Fatal("上一段的目标必须被携带")
	}
	if len(st.Cast["lin"].Knows) != 1 {
		t.Fatalf("知情范围必须累积：%v", st.Cast["lin"].Knows)
	}
	if st.Cast["career"].Resources != "协查权限" {
		t.Fatal("本单元没提的人物也必须保留")
	}
	if st.StoryDay != 12 {
		t.Fatalf("story_day=%d", st.StoryDay)
	}
}

// 章合同里的 plant/payoff 要折算进账本 loops：这块以前有渲染器但从未写入。
func TestStateFromOutlineWritesLoops(t *testing.T) {
	raw := `unit_id: v01-U1
chapters:
  - chapter: 1
    plant: [FS-01]
  - chapter: 3
    payoff: [FS-01, FS-02]
`
	st := StateFromOutline(domain.UnitInfo{UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 3, OutlineRaw: raw},
		[]domain.ChapterDraft{{Chapter: 1}, {Chapter: 2}, {Chapter: 3}})

	byID := map[string]domain.LoopEntry{}
	for _, l := range st.Loops {
		byID[l.ID] = l
	}
	if byID["FS-01"].Status != "paid" || byID["FS-01"].Planted != 1 || byID["FS-01"].Paid != 3 {
		t.Fatalf("FS-01=%+v", byID["FS-01"])
	}
	if byID["FS-02"].Status != "paid" {
		t.Fatalf("FS-02=%+v", byID["FS-02"])
	}
}

// loops 只升不降：重写已回收的单元不能把 paid 退回 planted。
func TestMergeUnitLoopsNeverDowngradesPaid(t *testing.T) {
	base := []domain.LoopEntry{{ID: "FS-01", Status: "paid", Planted: 1, Paid: 3}}
	raw := "unit_id: v01-U1\nchapters:\n  - chapter: 1\n    plant: [FS-01]\n"
	got := StateFromOutlineWithBase(domain.UnitState{Loops: base},
		domain.UnitInfo{UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 1, OutlineRaw: raw}, nil)
	for _, l := range got.Loops {
		if l.ID == "FS-01" && l.Status != "paid" {
			t.Fatalf("paid 被降级：%+v", l)
		}
	}
}
