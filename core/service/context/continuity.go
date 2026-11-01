package contextsvc

import (
	"fmt"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// ContinuityStage selects how much of the state snapshot a stage may see.
//
// 写作与审校需要的东西不一样：写作要"他现在在哪、想要什么、有什么伤"，
// 多给知情范围只会诱导模型提前把 POV 不知道的事写出来；审校恰恰要
// 用知情范围判断"他凭什么知道"。所以按阶段裁剪，而不是一份全给。
type ContinuityStage string

const (
	ContinuityForWrite  ContinuityStage = "write"
	ContinuityForReview ContinuityStage = "review"
)

// ExtractContinuity renders the continuity snapshot in force when entering unit.
//
// 状态来自 continuity/state.yaml 的按单元段，取严格早于目标单元的最后一段：
// 续写与重写因此读到同一份前置状态，后续单元的结果不会倒灌。
func ExtractContinuity(snap domain.BookSnapshot, unit domain.UnitInfo, onStage []string, stage ContinuityStage) string {
	states, err := books.LoadState(snap.Summary.RootPath)
	if err != nil || len(states) == 0 {
		return ""
	}
	st, ok := books.StateAt(states, unit.UnitID)
	if !ok {
		return ""
	}
	return renderContinuity(st, onStage, stage)
}

// renderContinuity formats one snapshot for the given stage.
func renderContinuity(st domain.UnitState, onStage []string, stage ContinuityStage) string {
	var out []string
	if head := continuityHeader(st); head != "" {
		out = append(out, head)
	}
	if people := continuityCast(st, onStage, stage); people != "" {
		out = append(out, people)
	}
	if loops := continuityLoops(st); loops != "" {
		out = append(out, loops)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n\n")
}

// ExtractContinuityLatest renders the newest snapshot in the ledger.
// Used when rewriting a canon file: there is no target unit, so "now" means the
// latest finalized state.
func ExtractContinuityLatest(snap domain.BookSnapshot, onStage []string, stage ContinuityStage) string {
	states, err := books.LoadState(snap.Summary.RootPath)
	if err != nil || len(states) == 0 {
		return ""
	}
	return renderContinuity(states[len(states)-1], onStage, stage)
}

// ExtractContinuityForUnit is the outline-stage variant: it takes the earliest
// unit being planned as the entry point. Outlining a unit must see the state as
// it stands just before that unit, exactly like writing it would.
func ExtractContinuityForUnit(snap domain.BookSnapshot, unitIDs []string, onStage []string, stage ContinuityStage) string {
	if len(unitIDs) == 0 {
		return ""
	}
	earliest := unitIDs[0]
	earliestKey := books.UnitOrderKey(earliest)
	for _, id := range unitIDs[1:] {
		if k := books.UnitOrderKey(id); k != 0 && (earliestKey == 0 || k < earliestKey) {
			earliest, earliestKey = id, k
		}
	}
	return ExtractContinuity(snap, domain.UnitInfo{UnitID: earliest}, onStage, stage)
}

func continuityHeader(st domain.UnitState) string {
	var parts []string
	if st.Volume > 0 {
		parts = append(parts, fmt.Sprintf("截至 %s（第 %d 卷）", st.Unit, st.Volume))
	} else {
		parts = append(parts, "截至 "+st.Unit)
	}
	if st.AfterCh > 0 {
		parts = append(parts, fmt.Sprintf("已定稿到第 %d 章", st.AfterCh))
	}
	if st.StoryDay > 0 {
		clock := fmt.Sprintf("故事日 %d", st.StoryDay)
		if st.EndDay > st.StoryDay {
			clock = fmt.Sprintf("故事日 %d–%d", st.StoryDay, st.EndDay)
		}
		parts = append(parts, clock)
	}
	return "### 连续性状态\n" + strings.Join(parts, " · ")
}

// continuityCast renders only on-stage people. Ordering is stable so the prompt
// hash stays comparable across runs with the same inputs.
func continuityCast(st domain.UnitState, onStage []string, stage ContinuityStage) string {
	if len(st.Cast) == 0 {
		return ""
	}
	order := append([]string{}, onStage...)
	if len(order) == 0 {
		for stem := range st.Cast {
			order = append(order, stem)
		}
		sort.Strings(order)
	}
	var rows []string
	for _, stem := range order {
		c, ok := st.Cast[stem]
		if !ok {
			continue
		}
		var parts []string
		if c.RoleTitle != "" {
			parts = append(parts, "身份="+c.RoleTitle)
		}
		if c.Location != "" {
			parts = append(parts, "位置="+c.Location)
		}
		if c.Goal != "" {
			parts = append(parts, "当前目标="+c.Goal)
		}
		if c.Injury != "" {
			parts = append(parts, "伤势="+c.Injury)
		}
		if c.Resources != "" {
			parts = append(parts, "资源="+c.Resources)
		}
		// 知情范围只给审校：写作看到它会把"他知道 X"直接写进旁白或让他猜中。
		if stage == ContinuityForReview && len(c.Knows) > 0 {
			// 知情范围只列要点：细纲里的整段论证放进提示词会挤掉正文的注意力。
			known := make([]string, 0, len(c.Knows))
			for _, k := range c.Knows {
				known = append(known, clipRunes(strings.TrimSpace(k), 60))
			}
			parts = append(parts, "知情范围="+strings.Join(known, "；"))
		}
		if len(parts) == 0 {
			continue
		}
		rows = append(rows, "- "+stem+"："+strings.Join(parts, "，"))
	}
	if len(rows) == 0 {
		return ""
	}
	return "### 人物当前状态\n" + strings.Join(rows, "\n")
}

// continuityLoops lists unresolved foreshadow threads. Paid ones are dropped:
// they are history, not constraints on what may be written next.
func continuityLoops(st domain.UnitState) string {
	var rows []string
	for _, loop := range st.Loops {
		if strings.EqualFold(loop.Status, "paid") {
			continue
		}
		row := "- " + loop.ID
		if loop.Status != "" {
			row += "（" + loop.Status + "）"
		}
		if loop.Detail != "" {
			row += "：" + clipRunes(loop.Detail, 80)
		}
		rows = append(rows, row)
		if len(rows) >= 6 {
			break
		}
	}
	if len(rows) == 0 {
		return ""
	}
	return "### 未回收伏笔（最多 6 条）\n" + strings.Join(rows, "\n")
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
