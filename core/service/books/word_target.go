package books

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// WordTargetPerChapter derives the per-chapter target from the unit outline's
// word_target field. word_target is the unit total; dividing by chapter count
// gives the per-chapter floor used by local gates. Falls back to 2000 when
// the field is missing or unparseable (existing behaviour).
func WordTargetPerChapter(unit domain.UnitInfo) int {
	re := regexp.MustCompile(`(?m)^word_target:\s*(.+)$`)
	m := re.FindStringSubmatch(unit.OutlineRaw)
	if len(m) < 2 {
		return 2000
	}
	val := strings.TrimSpace(m[1])
	val = strings.Trim(val, `"'`)
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return 2000
	}
	chapters := unit.ChapterTo - unit.ChapterFrom + 1
	if chapters <= 0 {
		chapters = 1
	}
	return n / chapters
}
