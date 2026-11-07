package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// MissingIdentityStems 与 ValidateIdentityCoverage 必须同一判据：前者是后者的
// 「返回名单」版本，两边分叉会让「收窄 onStage」与「校验」给出不同答案。
func TestMissingIdentityStemsMatchesValidation(t *testing.T) {
	segs := []domain.IdentitySegment{
		{Stem: "wen", RoleTitle: "新警", Org: "派出所", FromUnit: "v01-U1", ToUnit: "v01-U1"},
		{Stem: "wen", RoleTitle: "刑警", Org: "分局", FromUnit: "v01-U2"},
	}
	cast := []domain.CastCard{
		{Stem: "wen", Status: domain.CastCanon},
		{Stem: "lu", Status: domain.CastCanon},
		{Stem: "newbie", Status: domain.CastCandidate},
	}
	unit2 := domain.UnitInfo{UnitID: "v01-U2"}
	unit1 := domain.UnitInfo{UnitID: "v01-U1"}

	// v01-U2 有生效段 → 无缺失
	if got := MissingIdentityStems(segs, cast, unit2, []string{"wen"}); len(got) != 0 {
		t.Fatalf("有生效段时不应报缺失: %v", got)
	}
	if err := ValidateIdentityCoverage(segs, cast, unit2, []string{"wen"}); err != nil {
		t.Fatalf("校验也应通过: %v", err)
	}

	// lu 全程没有段 → 缺失，且校验必须报错
	got := MissingIdentityStems(segs, cast, unit2, []string{"wen", "lu"})
	if len(got) != 1 || got[0] != "lu" {
		t.Fatalf("应只报 lu: %v", got)
	}
	if err := ValidateIdentityCoverage(segs, cast, unit2, []string{"wen", "lu"}); err == nil ||
		!strings.Contains(err.Error(), "lu") {
		t.Fatalf("校验必须失败并点名 lu: %v", err)
	}

	// 候选卡豁免：还没晋升 canon 的人物不参与身份校验
	if got := MissingIdentityStems(segs, cast, unit2, []string{"newbie"}); len(got) != 0 {
		t.Fatalf("候选卡应豁免: %v", got)
	}

	// 段只在 v01-U1 生效时，v01-U2 对 wen 仍然有段（开放区间），v01-U1 也有
	if got := MissingIdentityStems(segs, cast, unit1, []string{"wen"}); len(got) != 0 {
		t.Fatalf("v01-U1 应命中第一段: %v", got)
	}
}
