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
//
// 回证（evidence）对不上正文时**不阻断**：那只是"这段引文在正文里找不到"，
// 而迁移本身可能是对的——硬拦会让一次引号字形差异作废整份账本。真正该做的
// 是把它记进定稿日志，让作者知道哪几条没被核对过。
func ApplyFinalizationLedger(root string, unit domain.UnitInfo, chapters []domain.ChapterDraft, actual []domain.ChapterActual, findings []domain.ReviewFinding, identities ...[]domain.IdentitySegment) error {
	if len(chapters) == 0 || len(actual) != len(chapters) {
		return fmt.Errorf("定稿必须逐章提供已核对的实际结果")
	}
	var unverified []string
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
			if domain.CanonicalStateField(d.Field) == "" || domain.CanonicalStateField(d.Field) == "role_title" {
				return fmt.Errorf("第 %d 章实际账本字段 %s 不合法", ch.Chapter, d.Field)
			}
			if d.Confirmed && !containsVerbatim(ch.Body, d.Evidence, d.Stem) {
				unverified = append(unverified, fmt.Sprintf("第 %d 章 %s.%s", ch.Chapter, d.Stem, d.Field))
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
	log := renderCommitLog(unit, chapters, actual, findings, unverified)
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

func renderCommitLog(unit domain.UnitInfo, chapters []domain.ChapterDraft, actual []domain.ChapterActual, findings []domain.ReviewFinding, unverified []string) string {
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
	// 回证没命中正文的迁移必须留痕：作者接受时看到的是 advisory，日志里要能
	// 事后追溯"当时哪几条引文没核对上"，否则这条账本看起来一切都验过了。
	if len(unverified) == 0 {
		fmt.Fprintf(&b, "- 回证核对: %d 条全部命中正文\n", countDeltas(actual))
	} else {
		fmt.Fprintf(&b, "- 回证核对: %d 条未命中正文（已按作者确认接受）: %s\n",
			len(unverified), strings.Join(unverified, "；"))
	}
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

// countDeltas is the total number of state transitions the ledger carries.
func countDeltas(actual []domain.ChapterActual) int {
	n := 0
	for _, a := range actual {
		n += len(a.StateDeltas)
	}
	return n
}

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

// ContainsVerbatim reports whether evidence really quotes the body.
// Exported so the generation path can pre-check a whole-unit ledger before the
// author ever clicks accept: the accept path is the authority, but a ledger that
// cannot pass it should never reach the author as a candidate.
func ContainsVerbatim(body, evidence, stem string) bool {
	return containsVerbatim(body, evidence, stem)
}

// containsVerbatim reports whether evidence really quotes the body.
//
// 「逐字」是语义要求，不是排版要求。模型抄回证时常跨段落，而正文在段间有空行、
// 行首缩进和行尾空白；按字节比较会把"只差一个换行"判成没有证据，整份定稿因此
// 失败，作者在预览里看不出任何内容差异，只能重跑一遍再赌一次。
//
// 所以先按原文比，再按忽略空白比：字符本身仍必须逐字一致、顺序不变，不做模糊
// 匹配——回证必须是正文里真实存在的一句话，只是允许版式不同。
func containsVerbatim(body, evidence, stem string) bool {
	if strings.TrimSpace(evidence) == "" {
		return false
	}
	if strings.Contains(body, evidence) {
		return true
	}
	squeezedBody := squeezeSpace(body)
	if strings.Contains(squeezedBody, squeezeSpace(evidence)) {
		return true
	}
	// 标点归一：正文用 ASCII 双引号 "，模型回抄时常常写成 “ ”「」，破折号也常
	// 换成 —/──。实测一个单元里 34 条迁移只有 2 条不过，而它们与正文逐字相同、
	// 只差引号字形——但这种"只差字形"足以让整份整单元定稿作废并退回逐章，
	// 多花掉一次 1.4 万 token 的调用。归一只放宽标点与空白，实词一个字都不放过。
	if strings.Contains(foldEvidence(body), foldEvidence(evidence)) {
		return true
	}
	return leadingSubjectPronounMatches(foldEvidence(body), evidence, stem)
}

// leadingSubjectPronounMatches 处理"引文把人物名换成了代词"这一类差异。
//
// 好看的正文常常写「温念慈把信封接过来」，模型抄回证时写成「她把信封接过来」——
// 它确实在引用原文，只是把主语换成了代词。按逐字比较会把这种正常的引文判成
// 伪造：整份定稿失败，而候选里其余几十条迁移都是对的，作者只能重跑一遍。
//
// 只容忍**句首主语**这一处差异：把引文开头的主语（人物名或代词）摘掉后，剩余
// 部分必须逐字出现在正文里，且至少 12 字——太短的片段（"他走了"）可以到处撞上，
// 不能当作证据。正文其余部分一个字都不许变（不做模糊匹配），所以把句子改写过的
// 引文仍然过不了。
//
// 这条检查证明的是"这段文字确实在正文里"，不负责判断它是不是在说这个人；
// 逐字命中的那一路本来也不判归属。
func leadingSubjectPronounMatches(foldedBody, evidence, stem string) bool {
	rest, ok := stripLeadingSubject(evidence, stem)
	if !ok {
		return false
	}
	folded := foldEvidence(rest)
	if utf8.RuneCountInString(folded) < 12 {
		return false
	}
	return strings.Contains(foldedBody, folded)
}

// stripLeadingSubject removes a leading subject token (the stem name or a
// pronoun) so the rest of the quote can be matched against the body.
func stripLeadingSubject(evidence, stem string) (string, bool) {
	s := strings.TrimLeft(evidence, " \t\n\r\u3000")
	if stem != "" && strings.HasPrefix(s, stem) {
		return strings.TrimLeft(s[len(stem):], " \t\n\r\u3000"), true
	}
	for _, p := range subjectPronouns {
		if strings.HasPrefix(s, p) {
			return strings.TrimLeft(s[len(p):], " \t\n\r\u3000"), true
		}
	}
	return "", false
}

// subjectPronouns are the tokens a model substitutes for a character name at the
// head of a quoted sentence.
var subjectPronouns = []string{"他们", "她们", "它们", "我们", "你们", "她", "他", "它", "我", "你"}

// squeezeSpace drops layout whitespace (including the full-width space) so two
// renderings of the same sentence compare equal.
func squeezeSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\u3000', '\u00a0':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// foldEvidence squeezes whitespace and folds typographic variants of the same
// punctuation, so 「逐字」 tolerates how a quote is typeset but not what it says.
//
// 逐字要求的是"这段话真的在正文里"，不是"引号必须是同一个字形"。正文里用
// ASCII `"` 的对白，模型回抄时几乎总是写成 `“ ”`；破折号同理。这类差异会让
// 一条正确的回证被拒，而代价是整份定稿重跑。归一只覆盖标点与空白：
//
//   - 引号（“”‘’「」『』）→ ' ' 直接删掉：引号位置本来就随排版变化
//   - 破折号、连接号 → '-'
//   - 全角 ASCII 区（！–～）→ 半角
//
// 任何实词、数字、语序的改动都不在归一范围内，所以编造的引文仍然过不了。
func foldEvidence(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\u3000', '\u00a0', '\u00ad':
			continue
		case '\u2018', '\u2019', '\u201a', '\u201b', '\u201c', '\u201d', '\u201e', '\u201f',
			'\u300c', '\u300d', '\u300e', '\u300f', '"', '\'', '`':
			continue
		case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212', '\u2500', '\u2501':
			b.WriteRune('-')
			continue
		}
		if r >= '\uff01' && r <= '\uff5e' {
			b.WriteRune(r - 0xfee0)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
