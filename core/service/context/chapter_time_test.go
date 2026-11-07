package contextsvc

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func timeUnit(outline string) domain.UnitInfo {
	return domain.UnitInfo{
		UnitID: "v01-U2", Volume: 1, ChapterFrom: 7, ChapterTo: 9,
		OutlinePath: "outline/units/v01-U2.yaml", OutlineRaw: outline,
	}
}

// 同一天内的连续两章，后一章必须「接着上一章的时刻继续」，不得重新起一天。
//
// 这是「章首时间戳疲劳」的正面修复。实测 266 章里 47 章以早晨类词开头，同一
// 单元内甚至连续两章都用「天刚亮透」起句；成因是时间规则只写在单元级块里，
// 而它要求「开场必须体现时间间隔」，逐章注入后模型理解为每章都要报一次时间。
func TestChapterTimeEntrySameDayContinues(t *testing.T) {
	outline := `unit_id: v01-U2
status: accepted
chapters:
  - chapter: 7
    goal: 甲
    change: 乙
    cut_hook: 丙
    day_offset: 3
    start_time: "傍晚"
  - chapter: 8
    goal: 甲2
    change: 乙2
    cut_hook: 丙2
    day_offset: 3
    start_time: "夜里"
`
	line := chapterTimeEntry(timeUnit(outline), 8)
	if !strings.Contains(line, "同一天") {
		t.Fatalf("同一天的章必须点明承接关系: %q", line)
	}
	if !strings.Contains(line, "不得重新") {
		t.Fatalf("必须禁止重新起一天: %q", line)
	}
	if !strings.Contains(line, "夜里") {
		t.Fatalf("必须带上本章 plan 的切入时段: %q", line)
	}
}

// 跨天的章允许一次相对时间锚点，并说明间隔天数。
func TestChapterTimeEntryAcrossDaysGivesInterval(t *testing.T) {
	outline := `unit_id: v01-U2
status: accepted
chapters:
  - chapter: 7
    goal: 甲
    change: 乙
    cut_hook: 丙
    day_offset: 3
    start_time: "上午"
  - chapter: 8
    goal: 甲2
    change: 乙2
    cut_hook: 丙2
    day_offset: 6
    start_time: "午后"
`
	line := chapterTimeEntry(timeUnit(outline), 8)
	if !strings.Contains(line, "3 天") {
		t.Fatalf("跨天的章应给出间隔天数: %q", line)
	}
	if strings.Contains(line, "不得重新") {
		t.Fatalf("跨天不该套用同日承接措辞: %q", line)
	}
}

// 单元首章只交代一次单元间隔，不套用同日承接。
func TestChapterTimeEntryFirstChapterOfUnit(t *testing.T) {
	outline := `unit_id: v01-U2
status: accepted
chapters:
  - chapter: 7
    goal: 甲
    change: 乙
    cut_hook: 丙
    day_offset: 0
    start_time: "清晨"
`
	line := chapterTimeEntry(timeUnit(outline), 7)
	if !strings.Contains(line, "第一章") || !strings.Contains(line, "只此一次") {
		t.Fatalf("首章应说明间隔只交代一次: %q", line)
	}
}

// 细纲没写 start_time 时不要编造时段，但仍要禁止同日倒退。
func TestChapterTimeEntryWithoutStartTime(t *testing.T) {
	outline := `unit_id: v01-U2
status: accepted
chapters:
  - chapter: 7
    goal: 甲
    change: 乙
    cut_hook: 丙
    day_offset: 2
  - chapter: 8
    goal: 甲2
    change: 乙2
    cut_hook: 丙2
    day_offset: 2
`
	line := chapterTimeEntry(timeUnit(outline), 8)
	if strings.Contains(line, "从当天**清晨**") || strings.Contains(line, "从当天**上午**") {
		t.Fatalf("缺失时段时不得凭空指定一个时段: %q", line)
	}
	if !strings.Contains(line, "不得") {
		t.Fatalf("即使没有时段也要禁止同日倒退: %q", line)
	}
}

// 时段写入 chapterRule，才能随每次单章调用一起发出去（chapterRule 在 extra 尾部）。
func TestWriteChapterExtraCarriesTimeEntry(t *testing.T) {
	unit := timeUnit(`unit_id: v01-U2
status: accepted
chapters:
  - chapter: 7
    goal: 甲
    change: 乙
    cut_hook: 丙
    day_offset: 1
    start_time: "上午"
`)
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "demo"}, Units: []domain.UnitInfo{unit}}
	extra := WriteChapterExtra(snap, unit, 7, 2500, 2000, 3500, "", nil)
	rule, _ := extra["chapterRule"].(string)
	if !strings.Contains(rule, "上午") {
		t.Fatalf("chapterRule 必须带上本章切入时段: %q", rule)
	}
}
