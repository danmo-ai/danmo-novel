package books

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ValidateClockContinuity checks 剧情时钟 across the volume's units:
// story_day 必须单调不减（闪回单元除外），单元间隔不能为负。
// Returns advisory findings (不 blocking，因为回填期字段可暂缺)。
func ValidateClockContinuity(units []domain.UnitInfo, volume int) []domain.ReviewFinding {
	var findings []domain.ReviewFinding
	var prevDay int = -1
	var prevID string
	for _, u := range units {
		if u.Volume != volume {
			continue
		}
		raw := u.OutlineRaw
		dayStr := clockScalar(raw, "story_day")
		flashback := clockScalar(raw, "flashback") == "true"
		if dayStr == "" || dayStr == "null" {
			findings = append(findings, domain.ReviewFinding{
				ID: "clock-missing-" + u.UnitID, Severity: "advisory", Source: "local_gate", Lens: "clock",
				Title:      "故事日未回填",
				Evidence:   u.UnitID + " story_day 为空，剧情时钟待补",
				Suggestion: "按正文逐日锚回填 story_day（相对天数）",
			})
			continue
		}
		day, err := strconv.Atoi(dayStr)
		if err != nil {
			findings = append(findings, domain.ReviewFinding{
				ID: "clock-invalid-" + u.UnitID, Severity: "advisory", Source: "local_gate", Lens: "clock",
				Title:    "故事日非数值",
				Evidence: fmt.Sprintf("%s story_day=%q，应为相对天数", u.UnitID, dayStr),
			})
			continue
		}
		if prevDay >= 0 && day < prevDay && !flashback {
			findings = append(findings, domain.ReviewFinding{
				ID: "clock-regress-" + u.UnitID, Severity: "blocking", Source: "local_gate", Lens: "clock",
				Title:      "故事日倒退",
				Evidence:   fmt.Sprintf("%s story_day=%d 早于 %s 的 %d，且未标闪回", u.UnitID, day, prevID, prevDay),
				Suggestion: "核对时间顺序；若为倒叙请标 flashback: true",
			})
		}
		if day > 0 {
			prevDay = day
			prevID = u.UnitID
		}
	}
	return findings
}

func clockScalar(raw, key string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*(.*)$`)
	m := re.FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	v := strings.Split(m[1], " #")[0]
	return strings.Trim(strings.TrimSpace(v), `"'`)
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace
