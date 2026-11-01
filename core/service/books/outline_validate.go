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
	for _, e := range doc.Entities {
		var required []string
		switch e.Kind {
		case "分卷":
			required = []string{"title", "chapter_range", "purpose", "dramatic_question", "entry_state", "exit_state", "deliverables", "next_volume_entry"}
		case "单元":
			required = []string{"title", "chapter_range", "objective", "obstacle", "attempt", "setback", "choice", "cost", "outcome", "entry_cause", "exit_result", "state_changes", "thread_ids", "next_hook"}
			from, to := ChapterWindow(e.Value("chapter_range"))
			if from <= 0 || to < from || to-from+1 > 10 {
				return fmt.Errorf("%s.chapter_range：有效范围且不超过 10 章", e.ID)
			}
		case "转折", "节拍":
			required = []string{"event", "cause", "effect", "chapter_window"}
		case "身份变更":
			required = []string{"stem", "role_title", "org", "from_unit", "reason"}
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
	for _, pair := range [][2]string{{"chapter_range", "chapter_range"}, {"entry_state", "entry_state"}, {"exit_state", "exit_state"}, {"dramatic_question", "goal"}} {
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
	revealDoc, hasReveals := StructuredOutlineFor(snap, "canon/reveal-schedule.md")
	reveals := map[string]bool{}
	if hasReveals && !revealDoc.Legacy {
		for _, e := range revealDoc.EntitiesOf("揭示") {
			reveals[e.ID] = true
		}
	}
	for _, e := range doc.EntitiesOf("分卷") {
		for _, id := range append(splitList(e.Value("reveal_ids")), splitList(e.Value("forbidden_reveal_ids"))...) {
			if hasReveals && !reveals[id] {
				return fmt.Errorf("%s：揭示计划中不存在 %s", e.ID, id)
			}
		}
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
