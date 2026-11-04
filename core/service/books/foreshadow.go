package books

import (
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// Foreshadow 是登记表里一条伏笔的只读视图。
//
// 字段与 Markdown 一一对应，不做二次加工：写作者看到的内容、校验器判定的
// 内容、审校核对的内容都从这里取，避免三处各自解析一遍。
type Foreshadow struct {
	ID            string
	Content       string
	Type          string
	PlantVolume   string
	PlantUnit     string
	PlantChapter  int
	PayoffVolume  string
	PayoffUnit    string
	PayoffChapter int
	PayoffSummary string
	LockedTerms   string
	Status        string
	Note          string
}

// Foreshadows reads the registry from a snapshot. Returns nil when the book has
// no registry yet (a book created before the merge, or one mid-migration).
func Foreshadows(snap domain.BookSnapshot) []Foreshadow {
	doc, ok := StructuredOutlineFor(snap, ForeshadowSchedulePath)
	if !ok || doc.Legacy || len(doc.Diagnostics) > 0 {
		return nil
	}
	var out []Foreshadow
	for _, e := range doc.EntitiesOf(ForeshadowEntityKind) {
		out = append(out, Foreshadow{
			ID:            e.ID,
			Content:       strings.TrimSpace(e.Value("content")),
			Type:          strings.TrimSpace(e.Value("type")),
			PlantVolume:   strings.TrimSpace(e.Value("plant_volume")),
			PlantUnit:     strings.TrimSpace(e.Value("plant_unit")),
			PlantChapter:  atoiSafe(strings.TrimSpace(e.Value("plant_chapter"))),
			PayoffVolume:  strings.TrimSpace(e.Value("payoff_volume")),
			PayoffUnit:    strings.TrimSpace(e.Value("payoff_unit")),
			PayoffChapter: atoiSafe(strings.TrimSpace(e.Value("payoff_chapter"))),
			PayoffSummary: strings.TrimSpace(e.Value("payoff_summary")),
			LockedTerms:   strings.TrimSpace(e.Value("locked_terms")),
			Status:        CanonicalForeshadowStatus(e.Value("status")),
			Note:          strings.TrimSpace(e.Value("note")),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PayoffPaid reports whether the thread has already been collected.
func (f Foreshadow) PayoffPaid() bool {
	return strings.EqualFold(f.Status, "paid")
}

// unitPosition 把单元 ID 压成可比较的整数；同一单元内章号为 0。
func unitPosition(unitID string) int {
	m := unitIDRe.FindStringSubmatch(strings.TrimSpace(unitID))
	if m == nil {
		return 0
	}
	v, _ := atoiOK(m[1])
	u, _ := atoiOK(m[2])
	return v*1_000_000 + u*10_000
}

func atoiOK(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// payoffPosition 是回收位置；只有卷时按该卷首个单元处理。
func (f Foreshadow) payoffPosition() int {
	vol := volumeNumber(f.PayoffVolume)
	if vol <= 0 {
		return 0
	}
	pos := vol * 1_000_000
	if idx := unitIndex(f.PayoffUnit); idx > 0 {
		pos += idx * 10_000
	}
	return pos + f.PayoffChapter
}

// ForeshadowUnitRefs derives the 可揭示 / 禁揭示 sets for one unit.
//
// 这两个集合此前是分卷与单元上的一对手写字段（reveal_ids /
// forbidden_reveal_ids），与登记表的回收位置是同一事实的两份记录。字段删掉后
// 由这里派生：回收位置不晚于本单元 → 本单元可以揭示；否则禁揭示。
func ForeshadowUnitRefs(snap domain.BookSnapshot, unitID string) (revealable, forbidden []string) {
	pos := unitPosition(unitID)
	if pos == 0 {
		return nil, nil
	}
	for _, f := range Foreshadows(snap) {
		payoff := f.payoffPosition()
		if payoff == 0 {
			continue
		}
		if payoff <= pos+9_999 { // 本单元任意章之前/之内完成回收
			revealable = append(revealable, f.ID)
			continue
		}
		forbidden = append(forbidden, f.ID)
	}
	return revealable, forbidden
}

// ForeshadowByID indexes the registry for lookup by unit-facing renderers.
func ForeshadowByID(snap domain.BookSnapshot) map[string]Foreshadow {
	out := map[string]Foreshadow{}
	for _, f := range Foreshadows(snap) {
		out[f.ID] = f
	}
	return out
}
