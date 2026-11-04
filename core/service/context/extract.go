package contextsvc

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
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

// 这里曾经有 ExtractRevealForVolume：把揭示计划按卷渲染成「可暗示/可确认」与
// 「禁止写穿」两段。它有两个致命问题，融合后整体删除：
//
//  1. 「禁止写穿」的条目也输出了 public_summary 全文。写第 38 章时提示词里
//     写着「陆沉来自 2024 年」「2009 年陆沉苏醒」——全书结局被直接交底。
//     AssertNoAuthorLore 只按文件来源判断，抓不到这种「合法文件里的剧透」。
//  2. 揭示计划本身已并入伏笔登记表（type=信息），「读者能否知道」由回收位置
//     派生，不再需要独立的渲染器。
//
// 现在的替代是 ForeshadowForChapter / ForeshadowForWriteUnit：
// 只给本章该埋该收的条目，未到回收点的条目连回收摘要都不给。

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

// ExtractLockedTerms returns actual forbidden words, never a pointer to their file.
func ExtractLockedTerms(raw, volumeID string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var file struct {
		LockedUntil map[string]any `yaml:"locked_until"`
		Compliance  []any          `yaml:"compliance"`
		Aliases     map[string]any `yaml:"aliases"`
	}
	if err := yaml.Unmarshal([]byte(raw), &file); err != nil {
		return "锁词配置无法解析：禁止定稿，先修复 canon/locked-terms.yaml。"
	}
	vol := 0
	fmt.Sscanf(strings.TrimPrefix(volumeID, "v"), "%d", &vol)
	rows := flattenStrings(file.Compliance)
	for unlock, value := range file.LockedUntil {
		n := 0
		fmt.Sscanf(strings.TrimPrefix(unlock, "v"), "%d", &n)
		// 到期卷已经允许使用；只有尚未到期的条目对当前章生效。
		if n > 0 && vol >= n {
			continue
		}
		rows = append(rows, flattenStrings([]any{value})...)
	}
	for term, alias := range file.Aliases {
		if slicesContains(rows, term) {
			rows = append(rows, flattenStrings([]any{alias})...)
		}
	}
	rows = uniqueSortedStrings(rows)
	if len(rows) == 0 {
		return "当前章无生效锁词；仍须遵守伏笔条目的回收边界。"
	}
	return "以下词语/别名当前禁止出现：" + strings.Join(rows, "；")
}

func flattenStrings(values []any) []string {
	var out []string
	for _, value := range values {
		switch v := value.(type) {
		case string:
			for _, part := range strings.Split(v, "；") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
		case []any:
			out = append(out, flattenStrings(v)...)
		case map[string]any:
			for _, key := range []string{"term", "word", "text", "alias", "rule"} {
				if text, ok := v[key]; ok {
					out = append(out, flattenStrings([]any{text})...)
				}
			}
		}
	}
	return out
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
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

// ChapterBeats prefers the structured scenes list, then chapters, then raw.
func ChapterBeats(raw string) string { return chapterBeats(raw) }

// ChapterContract returns the lightweight per-chapter plan and any optional
// scenes. It is the primary production input; scenes only clarify complex action.
func ChapterContract(raw string, chapter int) string {
	var doc struct {
		Chapters []struct {
			Chapter   int    `yaml:"chapter"`
			Goal      string `yaml:"goal"`
			Change    string `yaml:"change"`
			CutHook   string `yaml:"cut_hook"`
			DayOffset int    `yaml:"day_offset"`
			Scenes    []any  `yaml:"scenes"`
		} `yaml:"chapters"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return ""
	}
	for _, ch := range doc.Chapters {
		if ch.Chapter != chapter {
			continue
		}
		body, _ := yaml.Marshal(ch)
		return string(body)
	}
	return ""
}

func yamlScalar(raw, key string) string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*(.*)$`).FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"'`)
}

func chapterBeats(raw string) string {
	if chapters := mdYAMLList(raw, "chapters"); chapters != "" {
		return chapters
	}
	if scenes := mdYAMLList(raw, "scenes"); scenes != "" {
		return scenes
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
	if contract := ChapterContract(raw, chapter); contract != "" {
		return contract
	}
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
