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
//
// 职务身份不在这份账本里：它由 canon/identity-timeline.yaml 独占，这里按目标
// 单元现算（与人物卡上的「本单元身份」同源），避免两处各存一份副本后漂移。
func ExtractContinuity(snap domain.BookSnapshot, unit domain.UnitInfo, onStage []string, stage ContinuityStage) string {
	states, err := books.LoadState(snap.Summary.RootPath)
	if err != nil || len(states) == 0 {
		return ""
	}
	st, ok := books.StateAt(states, unit.UnitID)
	if !ok {
		return ""
	}
	// 久未更新的字段必须带上「记于何时」，否则模型会把它当成此刻的事实。
	// 实测一条 63 天前的伤势被当作当前身体状况写进了提示词。
	stale := map[string]int{}
	finalized := map[string]bool{}
	for _, s := range states {
		finalized[s.Unit] = true
	}
	var units []domain.UnitInfo
	for _, u := range snap.Units {
		if finalized[u.UnitID] {
			units = append(units, u)
		}
	}
	for _, f := range books.FindStaleCharacterFields(st, units) {
		stale[f.Stem+"."+f.Field] = f.UnitsAgo
	}
	return renderContinuity(st, snap.IdentitySegments, unit.UnitID, onStage, stage, stale)
}

// renderContinuity formats one snapshot for the given stage.
//
// identityUnitID 是「以哪个单元的身份为准」：写作/审校写的是目标单元，所以传
// 目标单元；重写设定文件时没有目标单元，传最新已定稿单元。
func renderContinuity(st domain.UnitState, identities []domain.IdentitySegment, identityUnitID string, onStage []string, stage ContinuityStage, stale map[string]int) string {
	var out []string
	if head := continuityHeader(st); head != "" {
		out = append(out, head)
	}
	if people := continuityCast(st, identities, identityUnitID, onStage, stage, stale); people != "" {
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
	last := states[len(states)-1]
	return renderContinuity(last, snap.IdentitySegments, last.Unit, onStage, stage, nil)
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
//
// 身份从身份时间线现取（IdentityAt），不从账本读——账本已经不存这份副本。
// 这样一来，人物卡上的「本单元身份」与这里永远是同一个值。
func continuityCast(st domain.UnitState, identities []domain.IdentitySegment, identityUnitID string, onStage []string, stage ContinuityStage, stale map[string]int) string {
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
		annex := func(field, label, value string) string {
			if value == "" {
				return ""
			}
			if ago, ok := stale[stem+"."+field]; ok {
				return label + "=" + value + fmt.Sprintf("（记于 %d 个单元前，可能已变）", ago)
			}
			return label + "=" + value
		}
		for _, item := range []struct{ field, label, value string }{
			{"location", "位置", c.Location},
			{"goal", "当前目标", c.Goal},
			{"health", "身体", c.Health},
			{"resources", "资源", c.Resources},
			{"relation", "关系", c.Relation},
		} {
			if v := annex(item.field, item.label, item.value); v != "" {
				parts = append(parts, v)
			}
		}
		// 生命状态必须一直可见：死者不该进 onStage，而这条是唯一能让写作者
		// 与审校同时看见的信号。
		if c.Life != "" && !strings.EqualFold(c.Life, "alive") {
			parts = append(parts, annex("life", "生命状态", c.Life))
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
