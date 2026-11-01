package books

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ApplyFinalizationLedger is the唯一写回路径 for定稿接受. It updates, in one
// commit: 单元状态 → reviewed、连续性状态段（continuity/state.yaml）、卷摘要、
// 定稿执行日志。只有 Accept 能调用它；生成阶段永不触碰这些文件。
//
// 不再写 facts.md：那份文件被逐轮追加成 571 KB 的散文日志，游标自相矛盾、
// 人物快照 30 多段从不合并，读写双方还因标题后缀静默失效。状态改由
// state.yaml 按单元段表达。
func ApplyFinalizationLedger(root string, unit domain.UnitInfo, chapters []domain.ChapterDraft, findings []domain.ReviewFinding) error {
	if len(chapters) == 0 {
		return fmt.Errorf("finalize: no chapters")
	}

	if err := SetUnitStatus(root, unit.UnitID, domain.UnitReviewed); err != nil {
		return err
	}

	// 连续性状态：从细纲的 state_deltas 落一段，供后续单元与重写读取。
	if err := AppendUnitState(root, StateFromOutline(unit, chapters)); err != nil {
		return err
	}

	if err := appendVolumeSummary(root, unit, chapters); err != nil {
		return err
	}
	return writeCommitLog(root, unit, chapters, findings)
}

// appendVolumeSummary upserts per-chapter 摘要 into continuity/summaries/vNN.md.
//
// 重写单元会再次定稿同一批章：旧实现按章追加，重写后同一章会出现两行。
// 现在先按上一次定稿账本记录的章范围删除旧行，再写入新行（幂等）。
func appendVolumeSummary(root string, unit domain.UnitInfo, chapters []domain.ChapterDraft) error {
	path := filepath.Join(root, "continuity", "summaries", fmt.Sprintf("v%02d.md", unit.Volume))
	var b strings.Builder
	if raw, err := os.ReadFile(path); err == nil {
		text := strings.TrimRight(string(raw), "\n")
		if from, to := committedChapterRange(root, unit.UnitID); from > 0 && to >= from {
			text = dropSummaryRows(text, from, to)
		}
		b.WriteString(text)
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "# 第 %d 卷章摘要\n\n| 章 | 事件 | 状态变化 | 伏笔 | 钩子 |\n|----|------|----------|------|------|\n", unit.Volume)
	}
	hook := scalarFromYAML(unit.OutlineRaw, "next_hook")
	if hook == "" {
		hook = mdYAMLMapValue(unit.OutlineRaw, "next_hook", "type")
	}
	for _, ch := range chapters {
		events := firstSentence(ch.Body, 60)
		state := scalarFromYAML(unit.OutlineRaw, "state_deltas")
		if state == "" {
			state = "见单元合同"
		}
		fmt.Fprintf(&b, "| %d | %s | %s | — | %s |\n", ch.Chapter, events, clipRunes(state, 30), clipRunes(hook, 20))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// writeCommitLog records the定稿执行日志 per unit.
func writeCommitLog(root string, unit domain.UnitInfo, chapters []domain.ChapterDraft, findings []domain.ReviewFinding) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# 定稿账本 · %s\n\n", unit.UnitID)
	fmt.Fprintf(&b, "- 定稿时间: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- 章范围: %d-%d\n", chapters[0].Chapter, chapters[len(chapters)-1].Chapter)
	total := 0
	for _, ch := range chapters {
		total += utf8.RuneCountInString(strings.ReplaceAll(ch.Body, " ", ""))
	}
	fmt.Fprintf(&b, "- 总字数: %d\n", total)
	blocking, advisory := 0, 0
	for _, f := range findings {
		if f.Severity == "blocking" {
			blocking++
		} else {
			advisory++
		}
	}
	fmt.Fprintf(&b, "- 门禁: blocking=%d advisory=%d\n", blocking, advisory)
	if hook := mdYAMLMapValue(unit.OutlineRaw, "next_hook", "type"); hook != "" {
		fmt.Fprintf(&b, "- 下一钩: %s\n", hook)
	}
	if deltas := mdYAMLSection(unit.OutlineRaw, "state_deltas"); deltas != "" {
		fmt.Fprintf(&b, "\n## state_deltas\n%s\n", deltas)
	}
	path := filepath.Join(root, "continuity", "commits", unit.UnitID+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ---- helpers ----

var chapterRowRe = regexp.MustCompile(`^\|\s*(\d+)\s*\|`)

// dropSummaryRows removes table rows whose 章 falls in [from, to].
func dropSummaryRows(text string, from, to int) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if m := chapterRowRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= from && n <= to {
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// committedChapterRange reads the previous 定稿账本's章范围 for a unit.
func committedChapterRange(root, unitID string) (int, int) {
	raw, err := os.ReadFile(filepath.Join(root, "continuity", "commits", unitID+".md"))
	if err != nil {
		return 0, 0
	}
	re := regexp.MustCompile(`章范围:\s*(\d+)\s*-\s*(\d+)`)
	m := re.FindStringSubmatch(string(raw))
	if m == nil {
		return 0, 0
	}
	from, err1 := strconv.Atoi(m[1])
	to, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return from, to
}

// mdYAMLSection returns the indented body of a top-level `key:` block.
//
// 必须用 [ \t]* 而不是 \s*：\s 会跨行匹配，把换行连同下一行的缩进一起吃掉，
// 于是返回的首行顶格、后续行仍带缩进，YAML 解析随即失败（state_deltas 就踩过）。
func mdYAMLSection(raw, key string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:[ \t]*\n((?:[ \t]+.*\n?)*)`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		// 只去尾部空行：TrimSpace 会连首行缩进一起吃掉，导致 YAML 列表首项顶格、
		// 与后续缩进项混排而解析失败。
		return strings.TrimRight(m[1], " \t\r\n")
	}
	return ""
}

func mdYAMLMapValue(raw, key, sub string) string {
	section := mdYAMLSection(raw, key)
	if section == "" {
		return ""
	}
	return scalarFromYAML(section, sub)
}

func scalarFromYAML(raw, key string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*(.*)$`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.Trim(strings.TrimSpace(m[1]), `"'`)
	}
	return ""
}

func mdSectionBody(raw, heading string) string {
	re := regexp.MustCompile(`(?ms)^` + regexp.QuoteMeta(heading) + `\s*\n(.*?)(?:\n#{2,3} |\z)`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func firstSentence(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "—"
	}
	if i := strings.IndexAny(s, "。！？!?\n"); i > 0 {
		s = s[:i+len("。")]
	}
	return clipRunes(s, max)
}

func clipRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
