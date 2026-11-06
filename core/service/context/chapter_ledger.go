package contextsvc

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// 上一章的「已接受事实账本」桥接。
//
// 与正文尾部的分工：
//   - previousChapterTail：800 字原文，只管语气、动作收尾与章末钩子。
//   - previousChapterLedger：已定稿账本投影出来的一小段，管「上一章实际发生了
//     什么」——地点/时间/人物状态迁移/伏笔埋收。
//
// 之所以单列这一项，是因为尾部是文字，模型会读漏：正文里已经出现过的楼层/日期/
// 时点被下一章开头改写（例如「三楼」写成「四楼」）。账本只放实际发生过的事，
// 不把计划当作事实，也不让审校候选充当已接受事实——审校是诊断，不是状态。
//
// 只在单元边界有效：本单元正在逐章串行写，上一章此刻还没有定稿账本，此时
// 回退到尾部即可，不能用未接受的候选内容冒充账本。
func PreviousChapterLedger(snap domain.BookSnapshot, unit domain.UnitInfo, chapter int) string {
	if unit.ChapterFrom > 0 && chapter > unit.ChapterFrom {
		// 单元内部衔接：上一章刚由本次 run 写出，尚未接受，不构成账本。
		return ""
	}
	prev := previousUnit(snap, unit)
	if prev == nil || snap.Summary.RootPath == "" {
		return ""
	}
	actual, err := books.LoadActual(snap.Summary.RootPath, prev.UnitID)
	if err != nil || len(actual) == 0 {
		return ""
	}
	// 账本必须正好覆盖到上一单元的末章，且末章就是本章的上一章；对不上说明
	// 账本还没有为这一单元结算完，或章号错位——宁可只发正文尾部，也不注入
	// 错位的"事实"（那会把另一章的地点/状态当成上一章的约束）。
	last := actual[len(actual)-1]
	if prev.ChapterTo > 0 && last.Chapter != prev.ChapterTo {
		return ""
	}
	if last.Chapter != chapter-1 {
		return ""
	}
	return formatChapterLedger(last)
}

// formatChapterLedger 把一章实际账本压成可执行的一小段。
//
// 只投影，不再判定：摘要与状态迁移在定稿接受时已经逐章核对过（见
// books.ApplyFinalizationLedger），这里若再补推断就成了第二份事实源。
func formatChapterLedger(a domain.ChapterActual) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("上一章（第 %d 章）已接受事实（来自已定稿账本，不得与正文冲突）：", a.Chapter))
	if s := strings.TrimSpace(a.Summary); s != "" {
		lines = append(lines, "- 实际事件："+s)
	}
	// 故事日/结束日只在账本体里出现过才写：不把计划时钟冒充实际时间。
	switch {
	case a.StoryDay > 0 && a.EndDay >= a.StoryDay:
		lines = append(lines, fmt.Sprintf("- 实际故事日：%d–%d", a.StoryDay, a.EndDay))
	case a.StoryDay > 0:
		lines = append(lines, fmt.Sprintf("- 实际故事日：%d", a.StoryDay))
	}
	for _, d := range a.StateDeltas {
		// 只投影已确认且有正文回证的迁移；未确认的"猜测"不能成为下一章的约束。
		if !d.Confirmed || strings.TrimSpace(d.Evidence) == "" || strings.TrimSpace(d.To) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s：%s → %s（%s）", d.Stem, d.From, d.To, d.Field))
	}
	if len(a.Plant) > 0 {
		lines = append(lines, "- 实际埋下伏笔："+strings.Join(a.Plant, "、"))
	}
	if len(a.Payoff) > 0 {
		lines = append(lines, "- 实际回收伏笔："+strings.Join(a.Payoff, "、"))
	}
	return strings.Join(lines, "\n")
}
