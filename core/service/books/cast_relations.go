package books

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// CastRelation is one undirected edge derived from cast cards.
type CastRelation struct {
	From    string
	To      string
	Type    string
	Current string
	Formal  bool // true = ## 关系 table row
}

var (
	relationAnchorRe = regexp.MustCompile(`(?m)^[\t *>-]*\*{0,2}关系锚点\*{0,2}[：:]\s*(.+)$`)
	proseRelationRe  = regexp.MustCompile(`(?m)^[\t *>-]*\*{0,2}(?:与主角关系|关键关系|关系钩)\*{0,2}[：:]\s*(.+)$`)
)

// ResolveCastRef maps a stem or Chinese display name to a cast stem.
func ResolveCastRef(ref string, cast []domain.CastCard) string {
	ref = strings.TrimSpace(strings.Trim(ref, "`\"'"))
	ref = strings.TrimSuffix(ref, "等")
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	for _, c := range cast {
		if c.Stem == ref {
			return c.Stem
		}
	}
	want := normalizePersonKey(ref)
	if want == "" {
		return ""
	}
	for _, c := range cast {
		if normalizePersonKey(c.Name) == want {
			return c.Stem
		}
		if normalizePersonKey(c.Stem) == want {
			return c.Stem
		}
	}
	// Prefix match for short honorifics like「陈组」→「陈组长」 when unique.
	var hit string
	for _, c := range cast {
		name := normalizePersonKey(c.Name)
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, want) || strings.HasPrefix(want, name) {
			if hit != "" && hit != c.Stem {
				return ""
			}
			hit = c.Stem
		}
	}
	return hit
}

// ParseCastRelations extracts formal relation tables and informal prose links.
func ParseCastRelations(cast []domain.CastCard) []CastRelation {
	var out []CastRelation
	seen := map[string]bool{}
	add := func(from, to, typ, current string, formal bool) {
		if from == "" || to == "" || from == to {
			return
		}
		key := from + "\x00" + to
		if from > to {
			key = to + "\x00" + from
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, CastRelation{From: from, To: to, Type: typ, Current: current, Formal: formal})
	}

	var protagonist string
	for _, c := range cast {
		role := strings.ToLower(strings.TrimSpace(c.Role))
		if role == "protagonist" || role == "hero" || role == "main" {
			protagonist = c.Stem
			break
		}
	}

	for _, card := range cast {
		for _, rel := range parseFormalRelationTable(card.Raw, cast) {
			add(card.Stem, rel.to, rel.typ, rel.current, true)
		}
		for _, rel := range parseRelationAnchor(card.Raw, cast) {
			add(card.Stem, rel.to, rel.typ, rel.current, false)
		}
		if protagonist != "" && card.Stem != protagonist {
			if m := proseRelationRe.FindStringSubmatch(card.Raw); len(m) > 1 {
				text := strings.TrimSpace(m[1])
				if text != "" {
					add(card.Stem, protagonist, "与主角", text, false)
				}
			}
		}
	}
	return out
}

type parsedRel struct {
	to, typ, current string
}

func parseFormalRelationTable(raw string, cast []domain.CastCard) []parsedRel {
	section := mdSectionBody(raw, "## 关系")
	if section == "" {
		section = mdSectionBody(raw, "## 人物关系")
	}
	if section == "" {
		return nil
	}
	var out []parsedRel
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}
		cells := splitTableCells(line)
		if len(cells) < 2 {
			continue
		}
		head := cells[0]
		if head == "对方" || head == "角色" || head == "人物" || head == "stem" {
			continue
		}
		to := ResolveCastRef(head, cast)
		if to == "" {
			continue
		}
		typ := ""
		current := ""
		if len(cells) > 1 {
			typ = cells[1]
		}
		if len(cells) > 2 {
			current = cells[2]
		}
		out = append(out, parsedRel{to: to, typ: typ, current: current})
	}
	return out
}

func parseRelationAnchor(raw string, cast []domain.CastCard) []parsedRel {
	m := relationAnchorRe.FindStringSubmatch(raw)
	if len(m) < 2 {
		return nil
	}
	body := strings.TrimSpace(m[1])
	var out []parsedRel
	for _, part := range splitAnchorParts(body) {
		names, desc := splitNamesAndDesc(part)
		if len(names) == 0 {
			continue
		}
		typ := "关系锚点"
		if desc != "" {
			typ = clipRunes(desc, 16)
		}
		for _, name := range names {
			to := ResolveCastRef(name, cast)
			if to == "" {
				continue
			}
			out = append(out, parsedRel{to: to, typ: typ, current: desc})
		}
	}
	return out
}

func splitAnchorParts(body string) []string {
	body = strings.ReplaceAll(body, ";", "；")
	var parts []string
	for _, p := range strings.Split(body, "；") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitNamesAndDesc(part string) (names []string, desc string) {
	part = strings.TrimSpace(part)
	if part == "" {
		return nil, ""
	}
	head := part
	if open := strings.IndexAny(part, "（("); open >= 0 {
		close := strings.IndexAny(part[open+1:], "）)")
		if close >= 0 {
			close = open + 1 + close
			head = strings.TrimSpace(part[:open])
			desc = strings.TrimSpace(part[open+1 : close])
		}
	}
	head = strings.ReplaceAll(head, "、", ",")
	head = strings.ReplaceAll(head, "，", ",")
	head = strings.ReplaceAll(head, "/", ",")
	for _, n := range strings.Split(head, ",") {
		n = strings.TrimSpace(n)
		n = strings.TrimSuffix(n, "等")
		n = strings.TrimSpace(n)
		if n != "" {
			names = append(names, n)
		}
	}
	return names, desc
}

func splitTableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func normalizePersonKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimSpace(s)
	// Drop parenthetical notes: 陆沉（…） / 温念慈(…)
	if i := strings.IndexAny(s, "（("); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.Is(unicode.Han, r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
