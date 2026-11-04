package books

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 这里曾经有 ValidateRevealSchedule：校验揭示计划（reveal_id/public_summary/
// earliest_volume/hint_unit_ids/confirm_unit_id/status/locked_terms）。
// 揭示计划已并入伏笔登记表（type=信息），旧 schema 不再存在，
// 校验由 ValidateForeshadowSchedule 统一承担，旧文件由迁移删除。

// ValidateCastCardContract checks the existing card syntax in place. It does
// not demand a wholesale YAML/front-matter migration of authored cards.
func ValidateCastCardContract(c domain.CastCard) error {
	if !castStemRe.MatchString(c.Stem) {
		return fmt.Errorf("人物 stem 非法: %s", c.Stem)
	}
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Role) == "" {
		return fmt.Errorf("人物卡 %s 缺少姓名或 role", c.Stem)
	}
	if c.Role != "protagonist" && c.Role != "volume_antagonist" && c.Role != "recurring" {
		return fmt.Errorf("人物卡 %s role 非法: %s", c.Stem, c.Role)
	}
	if c.Status != domain.CastCanon && c.Status != domain.CastCandidate {
		return fmt.Errorf("人物卡 %s status 非法", c.Stem)
	}
	return nil
}
