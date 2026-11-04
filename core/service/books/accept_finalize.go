package books

import (
	"crypto/sha256"
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
// commit: 单元状态 → finalized、连续性状态段（continuity/state.yaml）、卷摘要、
// 定稿执行日志。只有 Accept 能调用它；生成阶段永不触碰这些文件。
//
// 不再写 facts.md：那份文件被逐轮追加成 571 KB 的散文日志，游标自相矛盾、
// 人物快照 30 多段从不合并，读写双方还因标题后缀静默失效。状态改由
// state.yaml 按单元段表达。
func ApplyFinalizationLedger(root string, unit domain.UnitInfo, chapters []domain.ChapterDraft, actual []domain.ChapterActual, findings []domain.ReviewFinding, identities ...[]domain.IdentitySegment) error {
	if len(chapters) == 0 || len(actual) != len(chapters) {
		return fmt.Errorf("定稿必须逐章提供已核对的实际结果")
	}
	for i, ch := range chapters {
		if actual[i].Chapter != ch.Chapter || strings.TrimSpace(actual[i].Summary) == "" {
			return fmt.Errorf("第 %d 章缺少已核对的实际摘要", ch.Chapter)
		}
		plannedPlant, plannedPayoff := ChapterLoops(unit)
		for _, id := range append(append([]string{}, plannedPlant[ch.Chapter]...), plannedPayoff[ch.Chapter]...) {
			if !containsString(actual[i].PlannedForeshadowHandled, id) {
				return fmt.Errorf("第 %d 章计划伏笔 %s 尚未核对是否实际发生", ch.Chapter, id)
			}
		}
		for _, d := range actual[i].StateDeltas {
			if d.Confirmed && !strings.Contains(ch.Body, d.Evidence) {
				return fmt.Errorf("第 %d 章状态迁移 %s.%s 没有逐字正文证据", ch.Chapter, d.Stem, d.Field)
			}
			if domain.CanonicalStateField(d.Field) == "" || domain.CanonicalStateField(d.Field) == "role_title" {
				return fmt.Errorf("第 %d 章实际账本字段 %s 不合法", ch.Chapter, d.Field)
			}
		}
	}
	states, err := RebuildStateFromActual(root, unit.UnitID, actual)
	if err != nil {
		return err
	}
	stateRaw, err := marshalState(states)
	if err != nil {
		return err
	}
	actualRaw, err := MarshalActual(unit.UnitID, actual)
	if err != nil {
		return err
	}
	log := renderCommitLog(unit, chapters, actual, findings)
	var outlineRaw []byte
	if raw, readErr := os.ReadFile(filepath.Join(root, unit.OutlinePath)); readErr == nil {
		outlineRaw = []byte(regexp.MustCompile(`(?m)^status:.*$`).ReplaceAllString(string(raw), "status: "+string(domain.UnitFinalized)))
	} else {
		return readErr
	}
	statePath := filepath.Join(root, "novel-state.yaml")
	stateText, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}
	cursor := regexp.MustCompile(`(?m)^last_committed_ch:\s*.*$`).ReplaceAllString(string(stateText), "last_committed_ch: "+strconv.Itoa(chapters[len(chapters)-1].Chapter))
	return AtomicWriteFiles(root, map[string][]byte{
		StatePath: stateRaw, ActualPath(unit.UnitID): actualRaw,
		"continuity/commits/" + unit.UnitID + ".md": []byte(log),
		unit.OutlinePath: outlineRaw, "novel-state.yaml": []byte(cursor),
	})
}

func markdownCell(raw string) string {
	return strings.NewReplacer("|", "\\|", "\r", " ", "\n", " ").Replace(strings.TrimSpace(raw))
}

func renderCommitLog(unit domain.UnitInfo, chapters []domain.ChapterDraft, actual []domain.ChapterActual, findings []domain.ReviewFinding) string {
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
	hash := sha256.New()
	for _, ch := range chapters {
		fmt.Fprintf(hash, "%d\x00%s\x00%s\x00", ch.Chapter, ch.Title, ch.Body)
	}
	fmt.Fprintf(&b, "- 正文指纹: %x\n", hash.Sum(nil))
	fmt.Fprintf(&b, "- 实际事实索引: %s\n", ActualPath(unit.UnitID))
	return b.String()
}

// ---- helpers ----

var chapterRowRe = regexp.MustCompile(`^\|\s*(\d+)\s*\|`)

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

func clipRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
