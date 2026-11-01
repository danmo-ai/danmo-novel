package books

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ValidateRevealSchedule enforces the public reveal registry without reading
// or exposing the author-only truth stored in author-lore.md.
func ValidateRevealSchedule(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	doc := ParseStructuredMarkdown(raw, "reveal-schedule/v1")
	if doc.Legacy {
		return fmt.Errorf("揭示计划需要 Agent 迁移为结构化 Markdown")
	}
	if len(doc.Diagnostics) > 0 {
		return fmt.Errorf("揭示计划结构错误：%s", FormatOutlineDiagnostics(doc.Diagnostics))
	}
	for _, e := range doc.EntitiesOf("揭示") {
		for _, key := range []string{"public_summary", "earliest_volume", "hint_unit_ids", "confirm_unit_id", "status"} {
			if e.Value(key) == "" {
				return fmt.Errorf("揭示[%s].%s：必填字段为空", e.ID, key)
			}
		}
		if !volumeTagPattern.MatchString(e.Value("earliest_volume")) {
			return fmt.Errorf("揭示[%s].earliest_volume：需要 v01 形式", e.ID)
		}
		switch e.Value("status") {
		case "locked", "planted", "advanced", "paid", "dropped":
		default:
			return fmt.Errorf("揭示[%s].status：未知状态 %s", e.ID, e.Value("status"))
		}
	}
	return nil
}

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
