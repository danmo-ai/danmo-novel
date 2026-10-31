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
// commit: 单元状态 → reviewed、facts.md（游标/伏笔）、Cast snapshot、卷摘要、
// 定稿执行日志。只有 Accept 能调用它；生成阶段永不触碰这些文件。
func ApplyFinalizationLedger(root string, unit domain.UnitInfo, chapters []domain.ChapterDraft, findings []domain.ReviewFinding) error {
	if len(chapters) == 0 {
		return fmt.Errorf("finalize: no chapters")
	}
	last := chapters[len(chapters)-1].Chapter

	if err := SetUnitStatus(root, unit.UnitID, domain.UnitReviewed); err != nil {
		return err
	}

	factsPath := filepath.Join(root, "continuity", "facts.md")
	facts, _ := os.ReadFile(factsPath)
	factsText := string(facts)
	if factsText == "" {
		factsText = "# Continuity facts\n\n## Public facts\n| Kind | Fact | First seen | Notes |\n|------|------|------------|-------|\n\n## Tracking\n### Cursor\n- last_committed_ch: 0\n\n### Cast snapshot\n| 角色 | 年龄 | 职位 | 位置 | 目标 | 伤势/资源 | 知情范围 | 关系质态 |\n|------|------|------|------|------|-----------|----------|----------|\n\n## Open loops\n| FS-id | 内容 | 状态 | 埋点章 | 计划回收 | 实际回收 |\n|-------|------|------|--------|----------|----------|\n"
	}
	factsText = upsertCursor(factsText, last, unit)
	factsText = advanceOpenLoops(factsText, unit.OutlineRaw, last)
	factsText = upsertCastSnapshot(factsText, unit.OutlineRaw)
	if err := os.MkdirAll(filepath.Dir(factsPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(factsPath, []byte(factsText), 0o644); err != nil {
		return err
	}

	if err := appendVolumeSummary(root, unit, chapters); err != nil {
		return err
	}
	return writeCommitLog(root, unit, chapters, findings)
}

func upsertCursor(facts string, lastCh int, unit domain.UnitInfo) string {
	cursor := fmt.Sprintf("- last_committed_ch: %d\n- current volume / unit: v%02d / %s\n- next action: continue", lastCh, unit.Volume, unit.UnitID)
	re := regexp.MustCompile(`(?ms)(### Cursor\n)(.*?)(\n### |\n## |$)`)
	if loc := re.FindStringSubmatchIndex(facts); loc != nil {
		return facts[:loc[4]] + cursor + facts[loc[5]:]
	}
	if i := strings.Index(facts, "## Tracking"); i >= 0 {
		return facts[:i] + "## Tracking\n### Cursor\n" + cursor + "\n" + facts[i+len("## Tracking"):]
	}
	return facts + "\n## Tracking\n### Cursor\n" + cursor + "\n"
}

// advanceOpenLoops marks loops referenced by the unit's state_deltas as paid.
// state_deltas 中 "paid"/"payoff" 命中的 FS-id 会被推进；其余保持原状。
func advanceOpenLoops(facts, outlineRaw string, lastCh int) string {
	deltas := mdYAMLSection(outlineRaw, "state_deltas")
	if deltas == "" {
		return facts
	}
	loops := mdSectionBody(facts, "## Open loops")
	if loops == "" {
		return facts
	}
	var out []string
	for _, line := range strings.Split(loops, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") || strings.Contains(line, "---") {
			out = append(out, line)
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) >= 3 {
			id := strings.TrimSpace(cells[0])
			paidMarker := strings.Contains(deltas, "paid") || strings.Contains(deltas, "payoff")
			if id != "" && id != "FS-id" && strings.Contains(deltas, id) && paidMarker {
				cells[2] = " paid "
				if len(cells) >= 6 && strings.TrimSpace(cells[5]) == "" {
					cells[5] = fmt.Sprintf(" %d ", lastCh)
				}
				line = "|" + strings.Join(cells, "|") + "|"
			}
		}
		out = append(out, line)
	}
	return strings.Replace(facts, loops, strings.Join(out, "\n"), 1)
}

// upsertCastSnapshot refreshes rows for on-stage characters from the unit's
// state_deltas position/status hints; untouched characters keep their rows.
func upsertCastSnapshot(facts, outlineRaw string) string {
	onStage := extractYAMLListInline(outlineRaw, "on_stage")
	if len(onStage) == 0 {
		return facts
	}
	deltas := mdYAMLSection(outlineRaw, "state_deltas")
	if deltas == "" {
		deltas = outlineRaw // fallback: scan whole outline for hints
	}
	lines := strings.Split(facts, "\n")
	// locate the Cast snapshot table
	headIdx, sepIdx := -1, -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "### Cast snapshot" {
			headIdx = i
		}
		if headIdx >= 0 && sepIdx < 0 && strings.Contains(line, "---") {
			sepIdx = i
			break
		}
	}
	if sepIdx < 0 {
		return facts
	}
	have := map[string]bool{}
	for i := sepIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			break
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) >= 1 {
			have[strings.TrimSpace(cells[0])] = true
		}
	}
	insertAt := sepIdx + 1
	for insertAt < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[insertAt]), "|") {
		insertAt++
	}
	var added []string
	for _, stem := range onStage {
		if have[stem] {
			continue
		}
		pos := scalarNear(deltas, stem, "position")
		if pos == "" {
			pos = scalarNear(deltas, stem, "location")
		}
		state := scalarNear(deltas, stem, "status")
		added = append(added, fmt.Sprintf("| %s |  |  | %s |  | %s |  |  |", stem, pos, state))
	}
	if len(added) == 0 {
		return facts
	}
	out := append([]string{}, lines[:insertAt]...)
	out = append(out, added...)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n")
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

func mdYAMLSection(raw, key string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*\n((?:[ \t]+.*\n?)*)`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
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

func scalarNear(raw, stem, key string) string {
	re := regexp.MustCompile(`(?m)` + regexp.QuoteMeta(stem) + `[^\n]*` + regexp.QuoteMeta(key) + `[：:]\s*([^\n,;，；]+)`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.Trim(strings.TrimSpace(m[1]), `"'`)
	}
	return ""
}

func extractYAMLListInline(raw, key string) []string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*\[([^\]]*)\]`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		var out []string
		for _, p := range strings.Split(m[1], ",") {
			p = strings.Trim(strings.TrimSpace(p), `"'`)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
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
