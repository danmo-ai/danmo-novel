package books

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ValidateVolumeClockContract checks the time boundaries embedded in the
// volume's unit seeds. It is shared by generation, manual saves and Accept.
func ValidateVolumeClockContract(doc domain.StructuredMarkdown) error {
	units := doc.EntitiesOf("单元")
	if len(units) == 0 {
		return nil
	}
	events := map[string]bool{}
	for _, e := range doc.EntitiesOf("节拍") {
		events[e.ID] = true
	}
	for _, e := range doc.EntitiesOf("转折") {
		events[e.ID] = true
	}
	byTimeline := map[string][]domain.MarkdownEntity{}
	for _, e := range units {
		start, startErr := strconv.Atoi(strings.TrimSpace(e.Value("clock_start_day")))
		end, endErr := strconv.Atoi(strings.TrimSpace(e.Value("clock_end_day")))
		gap, gapErr := strconv.Atoi(strings.TrimSpace(e.Value("clock_gap_days")))
		if startErr != nil || endErr != nil || gapErr != nil || start <= 0 || end < start || gap < 0 {
			return fmt.Errorf("%s.clock：start/end/gap 必须为合法相对故事日，end >= start，gap >= 0", e.ID)
		}
		timeline := strings.TrimSpace(e.Value("timeline_id"))
		if timeline == "" {
			timeline = "main"
		}
		if timeline != "main" && e.Value("allow_flashback") != "true" {
			return fmt.Errorf("%s.timeline_id：非主线时间必须显式允许闪回/并行线", e.ID)
		}
		if deadline := strings.TrimSpace(e.Value("clock_deadline")); deadline != "" {
			parts := strings.Split(deadline, "@")
			if len(parts) != 2 || !events[parts[0]] {
				return fmt.Errorf("%s.clock_deadline：必须为已声明的 事件ID@故事日", e.ID)
			}
			day, err := strconv.Atoi(parts[1])
			if err != nil || day < start || day > end {
				return fmt.Errorf("%s.clock_deadline：死线必须落在本单元时间范围内", e.ID)
			}
		}
		byTimeline[timeline] = append(byTimeline[timeline], e)
	}
	for timeline, rows := range byTimeline {
		if timeline != "main" {
			continue
		}
		previousEnd := 0
		for _, e := range rows {
			start, _ := strconv.Atoi(e.Value("clock_start_day"))
			gap, _ := strconv.Atoi(e.Value("clock_gap_days"))
			if previousEnd > 0 && start < previousEnd+gap {
				return fmt.Errorf("%s.clock：与上一单元间隔不足或时间倒退", e.ID)
			}
			previousEnd, _ = strconv.Atoi(e.Value("clock_end_day"))
		}
	}
	return nil
}
