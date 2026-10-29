package contextsvc

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// Entity extraction is the single source of model-facing canon. No whole-card,
// whole-facts or author-lore reads are allowed into write/review packets.

// AuthorLorePatterns are substrings that mark a file/section as author-only.
// Write and review packets must never include blocks matching them.
var AuthorLorePatterns = []string{"author-lore", "作者底牌", "终局真相", "未来反转", "未揭规则"}

// IsAuthorOnlySource reports whether a source path/label is author-only lore.
func IsAuthorOnlySource(source string) bool {
	s := strings.ToLower(source)
	for _, p := range AuthorLorePatterns {
		if strings.Contains(s, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// AssertNoAuthorLore is the field-level禁读 boundary: write/review packets fail
// fast if an author-only source slipped into their context blocks.
func AssertNoAuthorLore(p *domain.ContextPacket) error {
	for _, b := range p.Blocks {
		if IsAuthorOnlySource(b.Source) || IsAuthorOnlySource(b.Label) {
			return fmt.Errorf("author-only content forbidden in %s packet: %s (%s)", p.TaskKind, b.ID, b.Source)
		}
	}
	return nil
}

// ExtractFactsForWrite pulls only reader-known facts relevant to the on-stage
// cast: 连载游标 + on-stage Cast snapshot rows + open loops mentioning the
// on-stage stems (fallback: first rows). Never returns the whole facts.md.
func ExtractFactsForWrite(raw string, onStage []string, unitID string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var out []string
	if cursor := mdSection(raw, "### Cursor"); cursor != "" {
		out = append(out, "### 连载游标\n"+clip(cursor, 900))
	}
	snapshot := mdSection(raw, "### Cast snapshot")
	rows := tableRows(snapshot)
	var people []string
	for _, row := range rows {
		for _, stem := range onStage {
			if strings.Contains(row[0], stem) || (displayName(stem) != "" && strings.Contains(row[0], displayName(stem))) {
				people = append(people, strings.Join(row, " | "))
				break
			}
		}
	}
	if len(people) > 0 {
		out = append(out, "### 当前人物状态\n"+strings.Join(people, "\n"))
	}
	loops := mdSection(raw, "## Open loops")
	var relevant, rest []string
	for _, row := range tableRows(loops) {
		joined := strings.Join(row, "|")
		hit := false
		for _, stem := range onStage {
			if stem != "" && strings.Contains(joined, stem) {
				hit = true
				break
			}
			if name := displayName(stem); name != "" && strings.Contains(joined, name) {
				hit = true
				break
			}
		}
		if hit {
			relevant = append(relevant, joined)
		} else {
			rest = append(rest, joined)
		}
	}
	loopRows := relevant
	if len(loopRows) == 0 {
		loopRows = rest
	}
	if len(loopRows) > 6 {
		loopRows = loopRows[:6]
	}
	if len(loopRows) > 0 {
		out = append(out, "### 相关公开伏笔（最多6条）\n"+strings.Join(loopRows, "\n"))
	}
	if len(out) == 0 {
		return "尚无已提交公开事实。"
	}
	return strings.Join(out, "\n\n")
}

// ExtractCastForWrite compresses a cast card to the fields a writer needs:
// name+role, 本单元职务身份, 三锚点, POV 知识边界, one voice sample, 关系.
// Never the whole card. identity 为该单元有效的职务身份（来自 identity-timeline）。
func ExtractCastForWrite(card domain.CastCard, pov string) string {
	return ExtractCastForWriteWithIdentity(card, pov, "")
}

func ExtractCastForWriteWithIdentity(card domain.CastCard, pov, identity string) string {
	raw := card.Raw
	var out []string
	out = append(out, fmt.Sprintf("人物：%s（%s, stem=%s）", cleanName(card.Name), NormalizeRole(card.Role), card.Stem))
	if identity != "" {
		out = append(out, "本单元身份："+identity)
	}
	for _, label := range []string{"视觉", "语言", "行为"} {
		if v := bulletValue(raw, label); v != "" {
			out = append(out, label+"："+v)
		}
	}
	if card.Stem == pov {
		if v := bulletValue(raw, "不知"); v != "" {
			out = append(out, "POV不知："+v)
		}
	}
	for _, label := range []string{"压力下", "日常", "掩饰"} {
		if v := bulletValue(raw, label); v != "" {
			out = append(out, "台词："+v)
			break
		}
	}
	if v := bulletValue(raw, "与主角关系"); v != "" {
		out = append(out, "关系："+v)
	}
	if len(out) == 1 {
		return clip(raw, 500)
	}
	return strings.Join(out, "\n")
}

func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "protagonist", "hero", "main":
		return "protagonist"
	case "antagonist", "volume_antagonist", "villain":
		return "volume_antagonist"
	default:
		return "recurring"
	}
}

func IsCanonProtagonist(card domain.CastCard) bool {
	return card.Status == domain.CastCanon && NormalizeRole(card.Role) == "protagonist"
}

func NormalizeUnitContract(raw string) string {
	// Keep model-facing clock fields explicit without altering source YAML.
	day := yamlScalar(raw, "story_day")
	gap := yamlScalar(raw, "gap_from_prev")
	flashback := yamlScalar(raw, "flashback")
	var clock []string
	if day != "" && day != "null" {
		clock = append(clock, "story_day="+day)
	}
	if gap != "" {
		clock = append(clock, "gap_from_prev="+gap)
	}
	if flashback != "" {
		clock = append(clock, "flashback="+flashback)
	}
	if len(clock) == 0 {
		return raw
	}
	return raw + "\n\n# 本单元故事时钟\n" + strings.Join(clock, " · ")
}

// ExtractStyle compresses the style fingerprint for write packets.
func ExtractStyle(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return clip(raw, 480)
}

// ExtractLockedTerms surfaces only the current volume's locked-term guidance
// without printing future secrets.
func ExtractLockedTerms(raw, volumeID string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	// Do not print future secrets; only show terms applicable before this volume.
	vol := 1
	fmt.Sscanf(strings.TrimPrefix(volumeID, "v"), "%d", &vol)
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\s{2}(v\d+):\s*$`).FindAllStringSubmatch(raw, -1) {
		n := 0
		fmt.Sscanf(strings.TrimPrefix(m[1], "v"), "%d", &n)
		if n >= vol {
			continue
		}
		out = append(out, m[1])
	}
	if len(out) == 0 {
		return "按卷锁词已配置；无额外当前卷锁词。"
	}
	return "当前卷锁词：见 canon/locked-terms.yaml 的 locked_until（到期卷 " + strings.Join(out, ", ") + "）。"
}

func mdSection(raw, heading string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(heading) + `\s*$`)
	loc := re.FindStringIndex(raw)
	if loc == nil {
		return ""
	}
	body := raw[loc[1]:]
	if next := regexp.MustCompile(`(?m)^#{2,3}\s`).FindStringIndex(body); next != nil {
		body = body[:next[0]]
	}
	return strings.TrimSpace(body)
}

func tableRows(raw string) [][]string {
	var out [][]string
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") || strings.Contains(line, "---") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 2 {
			continue
		}
		if strings.Contains(cells[0], "角色") || strings.Contains(cells[0], "FS-id") || strings.Contains(cells[0], "Kind") {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		out = append(out, cells)
	}
	return out
}

func bulletValue(raw, label string) string {
	re := regexp.MustCompile(`(?m)^\s*[-*]\s*(?:\*\*)?` + regexp.QuoteMeta(label) + `(?:\*\*)?[：:]\s*(.+)$`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

var roleParenRe = regexp.MustCompile(`\s*[（(][^）)]*(?:protagonist|antagonist|recurring|villain|hero|main|主角|反派|配角)[^）)]*[）)]\s*`)

// CleanName strips markdown heading marks and inline role annotations like
// "林尘 (protagonist)" / "林晚（主角）" so names never leak role metadata.
func CleanName(name string) string {
	name = strings.TrimSpace(regexp.MustCompile(`^#+\s*`).ReplaceAllString(name, ""))
	name = roleParenRe.ReplaceAllString(name, "")
	return strings.TrimSpace(name)
}

func cleanName(name string) string { return CleanName(name) }

func displayName(stem string) string { return stem }

// ChapterBeats prefers the structured scenes list, then chapters, then raw.
func ChapterBeats(raw string) string { return chapterBeats(raw) }

func yamlScalar(raw, key string) string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*(.*)$`).FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"'`)
}

func chapterBeats(raw string) string {
	if scenes := mdYAMLList(raw, "scenes"); scenes != "" {
		return scenes
	}
	if chapters := mdYAMLList(raw, "chapters"); chapters != "" {
		return chapters
	}
	return raw
}

func mdYAMLList(raw, key string) string {
	re := regexp.MustCompile(`(?m)^` + key + `:\s*\n((?:[ \t]+.*\n?)*)`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
