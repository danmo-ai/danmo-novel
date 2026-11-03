package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestIdentityChangeCreatesAndClosesSegments(t *testing.T) {
	initial := []domain.IdentitySegment{{Stem: "lin", RoleTitle: "内勤新警", Org: "派出所", FromUnit: "v01-U1"}}
	units := []domain.UnitInfo{{UnitID: "v01-U1", Volume: 1, Index: 1}, {UnitID: "v02-U1", Volume: 2, Index: 1}}
	segments, err := ApplyIdentityChange(initial, domain.IdentitySegment{Stem: "lin", RoleTitle: "重案队试用", Org: "分局", FromUnit: "v02-U1", Note: "调令"}, units)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].ToUnit != "v01-U1" {
		t.Fatalf("previous identity not closed: %+v", segments)
	}
	if got := IdentityAt(segments, "lin", "v02-U1"); got == nil || got.RoleTitle != "重案队试用" {
		t.Fatalf("new identity not effective: %+v", got)
	}
	if _, err := ApplyIdentityChange(segments, domain.IdentitySegment{Stem: "lin", RoleTitle: "专家", Org: "省厅", FromUnit: "v02-U1"}, units); err == nil {
		t.Fatal("overlapping identity must be rejected")
	}
}

func TestIdentityTimelineValidation(t *testing.T) {
	bad := []domain.IdentitySegment{{Stem: "lin", RoleTitle: "新警", Org: "", FromUnit: "v01-U1"}}
	if err := ValidateIdentityTimeline(bad, nil); err == nil {
		t.Fatal("missing org must be rejected")
	}
	if err := ValidateIdentityTimeline([]domain.IdentitySegment{{Stem: "lin", RoleTitle: "新警", Org: "所", FromUnit: "v01-U2", ToUnit: "v01-U1"}}, nil); err == nil {
		t.Fatal("reversed range must be rejected")
	}
}

func TestIdentityCoverageRequiresEffectiveSegment(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v02-U1"}
	segments := []domain.IdentitySegment{{Stem: "lin", RoleTitle: "内勤", Org: "所", FromUnit: "v01-U1", ToUnit: "v01-U8"}}
	if err := ValidateIdentityCoverage(segments, []domain.CastCard{{Stem: "lin", Status: domain.CastCanon}}, unit, []string{"lin"}); err == nil || !strings.Contains(err.Error(), "缺少") {
		t.Fatalf("missing segment must be actionable: %v", err)
	}
	if err := ValidateIdentityCoverage(segments, []domain.CastCard{{Stem: "lin", Status: domain.CastCandidate}}, unit, []string{"lin"}); err != nil {
		t.Fatalf("temporary candidate should be advisory, not blocking: %v", err)
	}
}

func TestFinalizedIdentityConfirmsWithoutCreatingSegments(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v02-U1", OutlineRaw: "on_stage: [lin]\nstate_deltas:\n  - stem: lin\n    field: role_title\n    from: 重案队试用\n    to: 重案队试用\n"}
	segments := []domain.IdentitySegment{{Stem: "lin", RoleTitle: "重案队试用", Org: "分局", FromUnit: "v02-U1"}}
	if err := ValidateFinalizedIdentity(segments, []domain.CastCard{{Stem: "lin", Status: domain.CastCanon}}, unit, unit.OutlineRaw); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(unit.OutlineRaw, "to: 重案队试用", "to: 擅自专家", 1)
	if err := ValidateFinalizedIdentity(segments, []domain.CastCard{{Stem: "lin", Status: domain.CastCanon}}, unit, bad); err == nil {
		t.Fatal("prose must not invent a new public identity")
	}
}

// 边界转换必须能定稿：from 是「单元前状态」，可以等于上一段身份。
//
// 真实事故：v01-U1 是「内勤新警」，v01-U2 起是「刑侦线新警」，U2 的
// state_deltas 写 from=内勤新警 / to=刑侦线新警——这正是细纲契约要求的
// 「单元前状态 → 单元后状态」。旧校验要求 from 也等于本单元生效身份，于是
// 整本正文写完后到定稿才被拦下，报错又指向"改时间线"，作者以为已定稿的设定要重写。
func TestFinalizedIdentityAllowsBoundaryTransition(t *testing.T) {
	segments := []domain.IdentitySegment{
		{Stem: "wen-nianci", RoleTitle: "内勤新警", Org: "双桥派出所", FromUnit: "v01-U1", ToUnit: "v01-U1"},
		{Stem: "wen-nianci", RoleTitle: "刑侦线新警", Org: "双桥派出所", FromUnit: "v01-U2", ToUnit: "v01-U8"},
	}
	cast := []domain.CastCard{{Stem: "wen-nianci", Status: domain.CastCanon}}
	unit := domain.UnitInfo{
		UnitID:     "v01-U2",
		OutlineRaw: "on_stage: [wen-nianci]\nstate_deltas:\n  - stem: wen-nianci\n    field: role_title\n    from: 内勤新警\n    to: 刑侦线新警\n",
	}
	if err := ValidateFinalizedIdentity(segments, cast, unit, unit.OutlineRaw); err != nil {
		t.Fatalf("a boundary transition described as 单元前→单元后 must be accepted: %v", err)
	}

	// to 仍必须等于本单元生效身份：写错就是真冲突，不能放行。
	wrongTo := strings.Replace(unit.OutlineRaw, "to: 刑侦线新警", "to: 重案队试用", 1)
	if err := ValidateFinalizedIdentity(segments, cast, unit, wrongTo); err == nil {
		t.Fatal("to must equal the unit's effective identity")
	}

	// from 既不是上一段也不是本单元 → state_deltas 写错了，必须报出来。
	garbage := strings.Replace(unit.OutlineRaw, "from: 内勤新警", "from: 特聘专家", 1)
	err := ValidateFinalizedIdentity(segments, cast, unit, garbage)
	if err == nil {
		t.Fatal("a from that matches neither the previous nor the current segment must be rejected")
	}
	// 报错要给出两个可选值，作者才知道该改成什么。
	if !strings.Contains(err.Error(), "内勤新警") || !strings.Contains(err.Error(), "刑侦线新警") {
		t.Fatalf("error should name both candidates, got: %v", err)
	}
}

func TestSaveIdentityTimeline(t *testing.T) {
	root := t.TempDir()
	if err := SaveIdentityTimeline(root, []domain.IdentitySegment{{Stem: "lin", RoleTitle: "新警", Org: "所", FromUnit: "v01-U1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, IdentityTimelinePath)); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIdentityTimeline(root)
	if err != nil || len(got) != 1 || got[0].RoleTitle != "新警" {
		t.Fatalf("roundtrip failed: %+v %v", got, err)
	}
}

func TestIdentityAt(t *testing.T) {
	segments := []domain.IdentitySegment{
		{Stem: "wen-nianci", RoleTitle: "内勤新警", Org: "派出所", FromUnit: "v01-U1", ToUnit: "v01-U7"},
		{Stem: "wen-nianci", RoleTitle: "重案队试用", Org: "分局", FromUnit: "v01-U8", ToUnit: "v02-U2"},
		{Stem: "wen-nianci", RoleTitle: "特聘专家", Org: "部里", FromUnit: "v02-U3"},
	}
	for _, c := range []struct{ unit, want string }{{"v01-U1", "内勤新警"}, {"v01-U8", "重案队试用"}, {"v02-U1", "重案队试用"}, {"v02-U3", "特聘专家"}, {"v03-U1", "特聘专家"}} {
		got := IdentityAt(segments, "wen-nianci", c.unit)
		if got == nil || got.RoleTitle != c.want {
			t.Fatalf("IdentityAt(%s)=%v want %s", c.unit, got, c.want)
		}
	}
}

func TestLoadIdentityTimeline(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "canon"), 0o755)
	raw := "segments:\n  - stem: lin\n    role_title: 内勤\n    org: 派出所\n    from_unit: v01-U1\n"
	_ = os.WriteFile(filepath.Join(root, IdentityTimelinePath), []byte(raw), 0o644)
	got, err := LoadIdentityTimeline(root)
	if err != nil || len(got) != 1 {
		t.Fatalf("load failed: %+v %v", got, err)
	}
}

// 单元内部身份恒定靠 state_deltas 的结构化比对判定，不靠关键词。
// 回归：合法的「不推晋升」口径与普通地点变化都不能被拦。
func TestIntraUnitIdentityUsesStructuredDeltas(t *testing.T) {
	segments := []domain.IdentitySegment{{Stem: "wen-nianci", RoleTitle: "见习警员", Org: "分局", FromUnit: "v01-U1"}}
	unit := domain.UnitInfo{UnitID: "v01-U2", Volume: 1}
	// 只是提到「晋升」这个词，身份没变 → 放行。
	pass := "state_deltas:\n  - stem: wen-nianci\n    field: goal\n    from: 不想晋升\n    to: 不推晋升、不给借调\n"
	if err := ValidateStateDeltasIdentity(segments, nil, unit, pass); err != nil {
		t.Fatalf("关键词不是身份转换，不该拦：%v", err)
	}
	// 真的改了 role_title → 拦。
	block := "state_deltas:\n  - stem: wen-nianci\n    field: role_title\n    from: 见习警员\n    to: 特聘专家\n"
	if err := ValidateStateDeltasIdentity(segments, nil, unit, block); err == nil {
		t.Fatal("role_title 改写必须被拦下")
	}
}

func TestValidateUnitIdentityOverlap(t *testing.T) {
	segments := []domain.IdentitySegment{{Stem: "lin", RoleTitle: "A", Org: "甲", FromUnit: "v01-U1", ToUnit: "v01-U5"}, {Stem: "lin", RoleTitle: "B", Org: "乙", FromUnit: "v01-U4"}}
	if blocking, _ := ValidateUnitIdentity(segments, []string{"lin"}, "v01-U4"); len(blocking) == 0 {
		t.Fatal("overlap should be blocking")
	}
}
