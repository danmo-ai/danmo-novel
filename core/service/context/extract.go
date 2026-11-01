package contextsvc

import (
	"fmt"
	"regexp"
	"strconv"
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

// ExtractVolumeHandoff compresses a previous volume outline into the bridge
// a next-volume plan needs: goals, cast stems, unit index tail, ending boundary.
// Never returns the whole previous volume prose or full unit contracts.
func ExtractVolumeHandoff(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var parts []string
	if title := firstHeading(raw); title != "" {
		parts = append(parts, "卷名："+title)
	}
	for _, h := range []string{"## 本卷目标", "## 卷目标", "## 卷命题", "## 本卷梗概"} {
		if body := mdSection(raw, h); body != "" {
			parts = append(parts, "### 本卷目标\n"+clip(body, 600))
			break
		}
	}
	for _, h := range []string{"## 本卷人物", "## 人物"} {
		if body := mdSection(raw, h); body != "" {
			parts = append(parts, "### 本卷人物\n"+clip(body, 400))
			break
		}
	}
	if idx := mdSection(raw, "## 单元索引"); idx != "" {
		rows := keepMarkdownTableTail(idx, 5)
		if rows != "" {
			parts = append(parts, "### 单元索引（末段）\n"+rows)
		}
	}
	for _, h := range []string{"## 卷末状态", "## 终局边界", "## 卷终局边界", "## 卷末收束", "## 卷末钩子"} {
		if body := mdSection(raw, h); body != "" {
			parts = append(parts, "### 卷末状态与边界\n"+clip(body, 700))
			break
		}
	}
	if len(parts) == 0 {
		return clip(raw, 1200)
	}
	return strings.Join(parts, "\n\n")
}

func firstHeading(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# "))
		}
	}
	return ""
}

func keepMarkdownTableTail(raw string, maxDataRows int) string {
	var header []string
	var data []string
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, "|") {
			continue
		}
		if strings.Contains(trim, "---") {
			if len(header) > 0 {
				header = append(header, trim)
			}
			continue
		}
		if len(header) == 0 {
			header = append(header, trim)
			continue
		}
		data = append(data, trim)
	}
	if len(data) == 0 {
		return clip(raw, 800)
	}
	if len(data) > maxDataRows {
		data = data[len(data)-maxDataRows:]
	}
	return strings.Join(append(header, data...), "\n")
}

// ExtractRevealForVolume returns the public reveal constraints effective for
// the given volume: 可暗示/可确认 for unlocked rows + 禁止写穿 for locked rows.
// 作者底牌细节不得写入本文件；只保留节奏与边界。Empty → no reveal block.
func ExtractRevealForVolume(raw string, volume int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if volume < 1 {
		volume = 1
	}
	allowed, locked := splitRevealRows(raw, volume)
	var out []string
	out = append(out, fmt.Sprintf("揭示约束（当前卷 v%02d）：只写已解锁信息；未解锁内容不得写穿、不得点名真名/真相。", volume))
	if len(allowed) > 0 {
		out = append(out, "可暗示/可确认：\n"+strings.Join(allowed, "\n"))
	}
	if len(locked) > 0 {
		out = append(out, "禁止写穿（锁后续卷）：\n"+strings.Join(locked, "\n"))
	}
	if len(allowed) == 0 && len(locked) == 0 {
		return clip(raw, 700)
	}
	return clip(strings.Join(out, "\n\n"), 1400)
}

// splitRevealRows best-effort parses reveal rows with a 最早卷/卷号 column.
// Rows without a parseable volume are treated as always-relevant boundary notes.
func splitRevealRows(raw string, volume int) (allowed, locked []string) {
	for _, line := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if strings.HasPrefix(s, "|") && (strings.Contains(s, "---") || strings.Contains(s, "信息") || strings.Contains(s, "底牌")) {
			continue
		}
		v := revealRowVolume(s)
		isRow := strings.HasPrefix(s, "|") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "*")
		if !isRow {
			continue
		}
		if v <= 0 {
			// 无卷号：永久边界（如「不得写穿」）保留为禁止项；其余略过。
			if strings.Contains(s, "禁") || strings.Contains(s, "不得") || strings.Contains(s, "锁") {
				locked = append(locked, clip(s, 200))
			}
			continue
		}
		if v <= volume {
			allowed = append(allowed, clip(s, 220))
		} else {
			locked = append(locked, clip(s, 200))
		}
	}
	if len(allowed) > 10 {
		allowed = allowed[:10]
	}
	if len(locked) > 8 {
		locked = locked[:8]
	}
	return allowed, locked
}

func revealRowVolume(s string) int {
	// v07 / v7 / 第7卷 / 卷7 / 最早卷: 7
	re := regexp.MustCompile(`(?i)(?:^|[^0-9a-z])v0*([0-9]{1,2})(?:[^0-9]|$)`)
	if m := re.FindStringSubmatch(s); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	re2 := regexp.MustCompile(`(?:第|卷|最早[^0-9]{0,6})([0-9]{1,2})`)
	if m := re2.FindStringSubmatch(s); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
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
	} else if rel := firstFormalRelationLine(raw); rel != "" {
		out = append(out, "关系："+rel)
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
	return clip(raw, 340)
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

func firstFormalRelationLine(raw string) string {
	section := mdSection(raw, "## 关系")
	if section == "" {
		section = mdSection(raw, "## 人物关系")
	}
	for _, row := range tableRows(section) {
		if len(row) < 2 || row[0] == "对方" || row[0] == "角色" || row[0] == "人物" {
			continue
		}
		cur := ""
		if len(row) > 2 {
			cur = row[2]
		}
		if cur != "" {
			return row[0] + " · " + row[1] + " · " + cur
		}
		return row[0] + " · " + row[1]
	}
	return ""
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

// dropYAMLList removes a top-level `key:` block (its indented body) from a
// unit outline. Used to keep scenes/chapters from being injected twice: the
// writer packet carries them in a dedicated 场面节拍 block, so repeating them
// inside the full contract only burns prompt budget.
func dropYAMLList(raw, key string) string {
	re := regexp.MustCompile(`(?m)^` + key + `:[ \t]*\n((?:[ \t]+.*\n?)*)`)
	return strings.TrimRight(re.ReplaceAllString(raw, ""), "\n")
}

// ContractWithoutBeats returns the unit contract with scenes/chapters removed,
// because ChapterBeats() already supplies them as a separate block.
// It falls back to the raw contract when the block is the only content, so a
// malformed outline never produces an empty contract.
func ContractWithoutBeats(raw string) string {
	out := raw
	for _, key := range []string{"scenes", "chapters"} {
		if mdYAMLList(out, key) == "" {
			continue
		}
		if trimmed := dropYAMLList(out, key); strings.TrimSpace(trimmed) != "" {
			out = trimmed
		}
	}
	return strings.TrimSpace(out)
}

var sceneStartRe = regexp.MustCompile(`(?m)^[ \t]*(?:- )?id:[ \t]*\S`)
var sceneChapterRe = regexp.MustCompile(`(?m)^[ \t]*chapter:[ \t]*(\d+)`)

// BeatsForChapter narrows the 场面节拍 block to one chapter's scenes.
//
// 分章写作时每章只需自己的 4–6 个场面，但 packet 是整单元构建的：6 章单元每次
// 调用都带全部 26 个场面，重复 6 遍。按 `chapter:` 过滤后单次提示词能省下大半。
// 没有 `chapter` 字段的场面一律保留——宁可多带，也不要把作者的节拍丢掉。
func BeatsForChapter(raw string, chapter int) string {
	block := ChapterBeats(raw)
	if chapter <= 0 || strings.TrimSpace(block) == "" {
		return block
	}
	locs := sceneStartRe.FindAllStringIndex(block, -1)
	if len(locs) < 2 {
		return block // 只有一段（或格式无法切分）时原样返回
	}
	var kept []string
	for i, loc := range locs {
		end := len(block)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		entry := strings.TrimRight(block[loc[0]:end], "\n")
		if m := sceneChapterRe.FindStringSubmatch(entry); len(m) == 2 {
			n, err := strconv.Atoi(m[1])
			if err == nil && n != chapter {
				continue
			}
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		return block // 过滤后为空说明 chapter 标记不可信，退回全量
	}
	return strings.Join(kept, "\n")
}

func mdYAMLList(raw, key string) string {
	re := regexp.MustCompile(`(?m)^` + key + `:\s*\n((?:[ \t]+.*\n?)*)`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
