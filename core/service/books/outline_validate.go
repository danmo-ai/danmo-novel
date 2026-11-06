package books

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ValidateStructuredOutlineFile is shared by manual saves and candidate Accept.
// Legacy files remain readable but are deliberately not silently promoted.
func ValidateStructuredOutlineFile(snap domain.BookSnapshot, path, raw string) error {
	if path != "outline/book_outline.md" && !(strings.HasPrefix(path, "outline/volumes/") && strings.HasSuffix(path, ".md")) {
		return nil
	}
	protocol := BookOutlineProtocol
	kind := "book"
	if path != "outline/book_outline.md" {
		protocol = VolumeOutlineProtocol
		kind = "volume"
	}
	doc := ParseStructuredMarkdown(raw, protocol)
	if doc.Legacy {
		return fmt.Errorf("旧格式大纲不进入生产链路，请先通过 Agent 批量迁移")
	}
	if len(doc.Diagnostics) > 0 {
		return fmt.Errorf("Markdown 结构校验失败：%s", FormatOutlineDiagnostics(doc.Diagnostics))
	}
	plannedUnits := append([]domain.UnitInfo(nil), snap.Units...)
	if kind == "volume" {
		for _, e := range doc.EntitiesOf("单元") {
			found := false
			for _, u := range plannedUnits {
				if u.UnitID == e.ID {
					found = true
					break
				}
			}
			if !found {
				plannedUnits = append(plannedUnits, domain.UnitInfo{UnitID: e.ID})
			}
		}
	}
	plannedIdentities := append([]domain.IdentitySegment(nil), snap.IdentitySegments...)
	// 第一遍只处理「身份变更」：单元种子的 role_title 一致性必须基于
	// 「本卷变更已应用」的时间线，而变更实体按约定写在所属单元之后。
	// 单遍循环会先校验单元、后应用变更——查的是一个本卷落盘前就已经过期的时间线，
	// 于是「本卷新增的身份」在校验里根本不存在，永远对不上。
	for _, e := range doc.Entities {
		if e.Kind != "身份变更" {
			continue
		}
		knownStem := false
		for _, c := range snap.Cast {
			if c.Stem == e.Value("stem") {
				knownStem = true
				break
			}
		}
		if len(snap.Cast) > 0 && !knownStem {
			return fmt.Errorf("身份变更[%s]：人物 %s 不存在，请先建立人物卡与初始身份", e.ID, e.Value("stem"))
		}
		if e.Value("from_unit") == "" || !strings.HasPrefix(e.ID, e.Value("from_unit")+"-") {
			return fmt.Errorf("身份变更[%s]：必须绑定所属单元边界", e.ID)
		}
		unitExists := false
		for _, u := range plannedUnits {
			if u.UnitID == e.Value("from_unit") {
				unitExists = true
				break
			}
		}
		if !unitExists {
			return fmt.Errorf("身份变更[%s]：起始单元不存在 %s", e.ID, e.Value("from_unit"))
		}
		var err error
		plannedIdentities, err = ApplyIdentityChange(plannedIdentities, domain.IdentitySegment{Stem: e.Value("stem"), RoleTitle: e.Value("role_title"), Org: e.Value("org"), FromUnit: e.Value("from_unit"), Note: e.Value("reason")}, plannedUnits)
		if err != nil {
			return fmt.Errorf("身份变更[%s]：%w", e.ID, err)
		}
		for _, field := range []string{"stem", "role_title", "org", "from_unit", "reason"} {
			if strings.TrimSpace(e.Value(field)) == "" {
				return fmt.Errorf("%s.%s：必填字段为空", e.ID, field)
			}
		}
	}
	for _, e := range doc.Entities {
		if e.Kind == "身份变更" {
			continue
		}
		var required []string
		switch e.Kind {
		case "分卷":
			required = []string{"title", "chapter_range", "purpose", "dramatic_question", "entry_state", "exit_state", "deliverables", "next_volume_entry"}
		case "单元":
			required = []string{"title", "chapter_range", "objective", "obstacle", "attempt", "setback", "choice", "cost", "outcome", "entry_cause", "exit_result", "state_changes", "clock_start_day", "clock_end_day", "clock_gap_days", "thread_ids", "next_hook"}
			from, to := ChapterWindow(e.Value("chapter_range"))
			if from <= 0 || to < from || to-from+1 > 10 {
				return fmt.Errorf("%s.chapter_range：有效范围且不超过 10 章", e.ID)
			}
			// 身份转换写错单元，细纲在数学上不可能通过（父级继承要求逐字包含
			// 这条迁移，而角色校验要求 to 等于本单元生效身份）。必须在卷纲落盘前拦住。
			if err := ValidateSeedIdentityChanges(plannedIdentities, e.ID, e.Value("state_changes")); err != nil {
				return err
			}
		case "转折", "节拍":
			required = []string{"event", "cause", "effect", "chapter_window"}
		}
		for _, field := range required {
			if strings.TrimSpace(e.Value(field)) == "" {
				return fmt.Errorf("%s.%s：必填字段为空", e.ID, field)
			}
		}
	}
	if kind == "book" {
		for _, field := range []string{"book_id", "title", "logline"} {
			if doc.Root.Value(field) == "" {
				return fmt.Errorf("%s：必填字段为空", field)
			}
		}
		if err := validateRanges(doc, "分卷", 0, 0); err != nil {
			return err
		}
		return validateReferences(snap, doc)
	}
	if err := ValidateVolumeClockContract(doc); err != nil {
		return err
	}
	if doc.Root.Value("cast") == "" {
		return fmt.Errorf("cast：本卷人物不能为空")
	}
	for _, field := range []string{"volume_id", "title", "source_arc_id", "chapter_range", "goal", "entry_state", "exit_state"} {
		if doc.Root.Value(field) == "" {
			return fmt.Errorf("%s：必填字段为空", field)
		}
	}
	parent, ok := StructuredOutlineFor(snap, "outline/book_outline.md")
	if !ok || !parent.Valid() {
		return fmt.Errorf("卷纲需要有效的结构化总纲")
	}
	from, to := ChapterWindow(doc.Root.Value("chapter_range"))
	if err := validateRanges(doc, "单元", from, to); err != nil {
		return err
	}
	arc, ok := parent.Entity("分卷", doc.Root.Value("volume_id"))
	if !ok {
		return fmt.Errorf("source_arc_id：总纲中不存在 %s", doc.Root.Value("volume_id"))
	}
	// 只锁定父级真正不可替换的合同。goal 是「本卷要完成的写作目标」，
	// dramatic_question 是「本卷要回答的读者悬念」——两者是不同概念，早期把它们
	// 绑成同一字段会逼模型把目标陈述改写成一句问句，等于用校验破坏内容。
	for _, pair := range [][2]string{{"chapter_range", "chapter_range"}, {"entry_state", "entry_state"}, {"exit_state", "exit_state"}, {"central_conflict", "central_conflict"}, {"stakes", "stakes"}, {"thread_moves", "thread_moves"}, {"deliverables", "deliverables"}} {
		if strings.TrimSpace(arc.Value(pair[0])) != strings.TrimSpace(doc.Root.Value(pair[1])) {
			return fmt.Errorf("%s：必须继承总纲的 %s", pair[1], pair[0])
		}
	}
	// Storyline IDs belong to the parent book, not to the individual volume.
	events := map[string]bool{}
	for _, e := range doc.EntitiesOf("节拍") {
		events[e.ID] = true
	}
	for _, e := range doc.EntitiesOf("转折") {
		events[e.ID] = true
	}
	for _, e := range doc.EntitiesOf("单元") {
		for _, id := range splitList(e.Value("thread_ids")) {
			if _, ok := parent.Entity("故事线", id); !ok {
				return fmt.Errorf("%s.thread_ids：总纲中不存在故事线 %s", e.ID, id)
			}
		}
		for _, id := range splitList(e.Value("event_ids")) {
			if !events[id] {
				return fmt.Errorf("%s.event_ids：引用不存在的卷级事件 %s", e.ID, id)
			}
		}
	}
	for _, stem := range splitList(doc.Root.Value("cast")) {
		found := false
		for _, c := range snap.Cast {
			if c.Stem == stem {
				found = true
				break
			}
		}
		if len(snap.Cast) > 0 && !found {
			return fmt.Errorf("cast：引用不存在的人物 %s", stem)
		}
	}
	if err := validateForeshadowRefs(snap, doc); err != nil {
		return err
	}
	return nil
}

// validateForeshadowRefs 校验总纲与卷纲引用的伏笔 ID 真实存在。
//
// 揭示融合后，分卷与单元不再各写一份 reveal_ids：哪些能揭示由登记表的回收
// 位置派生。但「本卷要埋/要收哪些伏笔」仍是规划阶段的显式决定，所以保留
// plant_ids / payoff_ids 两个引用字段，只校验它们指向真实条目。
func validateForeshadowRefs(snap domain.BookSnapshot, doc domain.StructuredMarkdown) error {
	known := map[string]bool{}
	for _, f := range Foreshadows(snap) {
		known[f.ID] = true
	}
	if len(known) == 0 {
		return nil
	}
	for _, kind := range []string{"分卷", "单元"} {
		for _, e := range doc.EntitiesOf(kind) {
			for _, field := range []string{"plant_ids", "payoff_ids"} {
				for _, id := range splitList(e.Value(field)) {
					if !known[id] {
						return fmt.Errorf("%s.%s：伏笔登记表中不存在 %s", e.ID, field, id)
					}
				}
			}
		}
	}
	return nil
}

func validateRanges(doc domain.StructuredMarkdown, kind string, outerFrom, outerTo int) error {
	entities := doc.EntitiesOf(kind)
	if len(entities) == 0 {
		return fmt.Errorf("%s：至少需要一个实体", kind)
	}
	previousTo := 0
	for _, e := range entities {
		from, to := ChapterWindow(e.Value("chapter_range"))
		if from <= 0 || to < from {
			return fmt.Errorf("%s.chapter_range：非法范围", e.ID)
		}
		if previousTo > 0 && from != previousTo+1 {
			return fmt.Errorf("%s.chapter_range：与上一实体存在缺口或重叠", e.ID)
		}
		if previousTo == 0 && outerFrom > 0 && from != outerFrom {
			return fmt.Errorf("%s.chapter_range：必须从第 %d 章开始", e.ID, outerFrom)
		}
		previousTo = to
	}
	if outerTo > 0 && previousTo != outerTo {
		return fmt.Errorf("%s：必须完整覆盖到第 %d 章", kind, outerTo)
	}
	return nil
}

func validateReferences(snap domain.BookSnapshot, doc domain.StructuredMarkdown) error {
	threads := map[string]bool{}
	for _, e := range doc.EntitiesOf("故事线") {
		threads[e.ID] = true
	}
	cast := map[string]bool{}
	for _, c := range snap.Cast {
		cast[c.Stem] = true
	}
	for _, e := range doc.EntitiesOf("单元") {
		for _, id := range splitList(e.Value("thread_ids")) {
			if !threads[id] {
				return fmt.Errorf("%s.thread_ids：引用不存在的故事线 %s", e.ID, id)
			}
		}
	}
	if doc.Protocol == VolumeOutlineProtocol {
		for _, stem := range splitList(doc.Root.Value("cast")) {
			if len(cast) > 0 && !cast[stem] {
				return fmt.Errorf("cast：引用不存在的人物 %s", stem)
			}
		}
	}
	return nil
}

// 这里曾经有 validateRevealRefs / checkRevealField：校验分卷与单元的
// reveal_ids / forbidden_reveal_ids 是否指向真实揭示条目。融合后这两个字段
// 已删除——它们记录的「本单元该收哪些」与伏笔登记表的 payoff_volume /
// payoff_unit 是同一事实的两份登记，现在由 ForeshadowUnitRefs 派生。
// 字段消失，冲突也就不可能发生，这段校验自然没有存在意义。
