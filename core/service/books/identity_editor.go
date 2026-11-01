package books

import (
	"fmt"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// UpdateIdentitySegment corrects an existing planning record. It does not
// delete history or silently retcon identities used by existing prose.
func UpdateIdentitySegment(segments []domain.IdentitySegment, index int, next domain.IdentitySegment, units []domain.UnitInfo) ([]domain.IdentitySegment, error) {
	if index < 0 || index >= len(segments) {
		return nil, fmt.Errorf("身份段不存在")
	}
	if next.Stem != segments[index].Stem || next.FromUnit != segments[index].FromUnit {
		return nil, fmt.Errorf("人物与起始单元是历史锚点，请通过新增身份段变更，不要移动既有历史")
	}
	for _, u := range units {
		if UnitOrderKey(u.UnitID) >= UnitOrderKey(next.FromUnit) && (next.ToUnit == "" || UnitOrderKey(u.UnitID) <= UnitOrderKey(next.ToUnit)) && u.HasProse {
			if next.RoleTitle != segments[index].RoleTitle || next.Org != segments[index].Org || next.ToUnit != segments[index].ToUnit {
				return nil, fmt.Errorf("%s 已有正文，修正身份段需通过影响预览与规划候选，不可直接覆盖", u.UnitID)
			}
		}
	}
	out := append([]domain.IdentitySegment(nil), segments...)
	out[index] = next
	if err := ValidateIdentityTimeline(out, units); err != nil {
		return nil, err
	}
	return out, nil
}

// CloseIdentitySegment ends the currently open segment at the last unit before
// a new boundary. The following segment must be added through planning Accept.
func CloseIdentitySegment(segments []domain.IdentitySegment, index int, toUnit string, units []domain.UnitInfo) ([]domain.IdentitySegment, error) {
	if index < 0 || index >= len(segments) {
		return nil, fmt.Errorf("身份段不存在")
	}
	if segments[index].ToUnit != "" {
		return nil, fmt.Errorf("历史身份段已结束")
	}
	if UnitOrderKey(toUnit) < UnitOrderKey(segments[index].FromUnit) {
		return nil, fmt.Errorf("结束单元不能早于起始单元")
	}
	for _, u := range units {
		if UnitOrderKey(u.UnitID) > UnitOrderKey(toUnit) && u.HasProse {
			return nil, fmt.Errorf("%s 已有正文依赖当前开放身份段，请先通过规划候选处理影响", u.UnitID)
		}
	}
	out := append([]domain.IdentitySegment(nil), segments...)
	out[index].ToUnit = toUnit
	if err := ValidateIdentityTimeline(out, units); err != nil {
		return nil, err
	}
	return out, nil
}
