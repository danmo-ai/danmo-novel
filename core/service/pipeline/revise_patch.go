package pipeline

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 定向修订的补丁契约。
//
// 以前修订必须回整章正文（2000+ 字），而一次修订通常只动一两句：输出 token
// 几乎全花在复述没变的文字上。改成补丁后，模型只给「改哪一段、改成什么」，
// 系统在原文上套用，输出量降到几条 hunk。
//
// 代价是定位。开源实现（aider/Cline/Codex）的共同教训是：**不要用模糊匹配猜**。
// aider 的 Levenshtein 匹配实现了又关掉，因为它会静默删掉语义重要的行；正文
// 删字不可逆，宁可报错回喂，也不能猜。所以这里只做字符层归一（空白、标点），
// 绝不做语义模糊，并且逐处计数——0 处和多处都是硬错误。

// chapterEdit is one hunk inside a single chapter. Two forms:
//
//   - 替换：find + replace（覆盖改字、删句、以及"在锚点旁插入"）
//   - 章末追加：append（不需要锚点）
//
// 追加必须单独一个形态，因为它是唯一无法用锚点可靠定位的位置：章末最后一句
// 常常是短句（「他走了。」），章内撞车时模型只能往回扩上下文，扩不出来就
// 只能落到整章兜底——而"续写式扩写"恰恰是最高频的扩写形态。
type chapterEdit struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
	Append  string `json:"append"`
}

// isAppend reports whether this edit only appends to the chapter end.
func (e chapterEdit) isAppend() bool { return e.Append != "" }

// chapterPatch is the revise output contract.
//
// chapter 是寻址维度而不只是一个字段：一次调用只带本章正文，所以唯一性只需
// 在**本章内**成立。跨章才会重复的模式（空行、对话起头、分隔线）在这里不再
// 构成歧义；回错章号仍然直接拒绝，绝不把别章内容写回。
type chapterPatch struct {
	Chapter int           `json:"chapter"`
	Title   string        `json:"title"`
	Edits   []chapterEdit `json:"edits"`
}

// 匹配层级：严格 → 宽松，命中即止。
//
// 每一层都是"字符层"的等价判定，不涉及任何语义近似：四层都能把同一处原文
// 认出来，只是容忍的书写差异不同（行尾空白、缩进、弯引号/破折号/全角空格）。
const (
	matchExact = iota
	matchRStrip
	matchTrim
	matchNormalized
)

var matchTierNames = [...]string{"逐字", "忽略行尾空白", "忽略首尾空白", "标点归一"}

// normalizeRune folds common typographic variants to their ASCII equivalents.
//
// 模型抄中文原文时最常走的样：破折号写成 '—' 而正文是 '──'、引号写成 " "
// 而正文是「」、全角空格写成半角、标点用半角 ':' 而正文是 '：'。这些是同一个
// 字符的不同写法，归一不会改变语义；而改动任何一个实词都不会被归一糊过去。
//
// 归一只会让比较更宽松，因此最多把「唯一命中」变成「多处命中」——那种情况
// 一律报错回喂，不会猜。所以放宽是安全的：它降低的是拒绝率，不是正确性。
func normalizeRune(r rune) rune {
	switch r {
	case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212', '\u2500', '\u2501':
		return '-'
	case '\u2018', '\u2019', '\u201A', '\u201B':
		return '\''
	case '\u201C', '\u201D', '\u201E', '\u201F', '\u300C', '\u300D', '\u300E', '\u300F':
		return '"'
	case '\u00A0', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006',
		'\u2007', '\u2008', '\u2009', '\u200A', '\u202F', '\u205F', '\u3000':
		return ' '
	}
	// 全角 ASCII 区（U+FF01–U+FF5E）整体回落到对应的 ASCII：：→:、，→,、；→;、
	// ！→!、「？→?」、（）→() 等。中文正文与模型输出在这里的分歧最频繁。
	if r >= '\uFF01' && r <= '\uFF5E' {
		return r - 0xFEE0
	}
	return r
}

// normView is the original text projected onto one matching tier.
//
// runes 是归一后的字符；orig[i] 是它在原文里的下标。两者等长，所以归一后命中
// 的区间可以直接映回原文区间，不需要二次搜索——偏移漂移是这类补丁最隐蔽的
// bug 来源，用映射表从结构上消除。
type normView struct {
	runes []rune
	orig  []int
}

func buildView(base []rune, tier int) normView {
	v := normView{runes: make([]rune, 0, len(base)), orig: make([]int, 0, len(base))}
	emit := func(i int) {
		r := base[i]
		if tier >= matchNormalized {
			r = normalizeRune(r)
		}
		v.runes = append(v.runes, r)
		v.orig = append(v.orig, i)
	}
	lineStart := 0
	for i := 0; i <= len(base); i++ {
		if i < len(base) && base[i] != '\n' {
			continue
		}
		lo, hi := lineStart, i
		if tier >= matchRStrip {
			for hi > lo && unicode.IsSpace(base[hi-1]) {
				hi--
			}
		}
		if tier >= matchTrim {
			for lo < hi && unicode.IsSpace(base[lo]) {
				lo++
			}
		}
		for j := lo; j < hi; j++ {
			emit(j)
		}
		if i < len(base) {
			emit(i) // 换行本身不参与裁剪，否则跨行锚点会对不上
		}
		lineStart = i + 1
	}
	return v
}

// findOccurrences returns every non-overlapping start offset of needle in hay.
func findOccurrences(hay, needle []rune) []int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return nil
	}
	var out []int
	for i := 0; i+len(needle) <= len(hay); {
		if runesEqual(hay[i:i+len(needle)], needle) {
			out = append(out, i)
			i += len(needle)
		} else {
			i++
		}
	}
	return out
}

func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// locatedEdit is one edit resolved to a rune span in the original chapter body.
type locatedEdit struct {
	index   int
	start   int
	end     int
	replace string
}

// locateEdit resolves find to exactly one span of the chapter body.
//
// 只有在某一层上**恰好**命中一处才算成功：命中多处时报错而不是取第一处，
// 因为"模型指的是哪一处"无法从输出推断，猜错等于把改动写到别的地方。
func locateEdit(base []rune, find string, idx int) (locatedEdit, error) {
	findRunes := []rune(find)
	for tier := matchExact; tier <= matchNormalized; tier++ {
		hay := buildView(base, tier)
		needle := buildView(findRunes, tier)
		hits := findOccurrences(hay.runes, needle.runes)
		switch len(hits) {
		case 0:
			continue
		case 1:
			start := hay.orig[hits[0]]
			end := hay.orig[hits[0]+len(needle.runes)-1] + 1
			return locatedEdit{index: idx, start: start, end: end, replace: find}, nil
		default:
			return locatedEdit{}, ambiguousEditError(base, find, idx, tier, hay, hits, len(needle.runes))
		}
	}
	return locatedEdit{}, missingEditError(base, find, idx)
}

// ambiguousEditError explains every hit so the model can widen its anchor.
//
// 只说"出现多次"模型改不动；给出行号与所在句子，它才知道该往左还是往右扩。
func ambiguousEditError(base []rune, find string, idx, tier int, hay normView, hits []int, size int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "edits[%d].find 在本章出现 %d 次（%s匹配），无法确定改哪一处：\n", idx, len(hits), matchTierNames[tier])
	for i, h := range hits {
		if i >= 3 {
			fmt.Fprintf(&b, "…还有 %d 处\n", len(hits)-3)
			break
		}
		at := hay.orig[h]
		fmt.Fprintf(&b, "- 第 %d 行：%s\n", lineOf(base, at), sentenceAround(base, at, size))
	}
	b.WriteString("请把 find 向左右各扩一句，使其在本章内唯一。")
	return fmt.Errorf("%s", b.String())
}

// missingEditError reports a find that matched nowhere, with real nearby text.
//
// aider 的做法（"Did you mean to match some of these actual lines?"）被证明
// 有效：模型据此发现自己抄错了字，一次重试就能改对。这里取最相似的 3 行。
func missingEditError(base []rune, find string, idx int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "edits[%d].find 在本章正文里找不到（已按逐字、空白、标点归一四种方式比对）。\n", idx)
	fmt.Fprintf(&b, "你给的片段：%s\n", clipRunes(strings.TrimSpace(find), 80))
	if r := runeLen(strings.TrimSpace(find)); r < shortAnchorRunes {
		fmt.Fprintf(&b, "该片段只有 %d 字，过短，几乎没有唯一命中的可能。\n", r)
	}
	if cands := similarLines(base, find, 3); len(cands) > 0 {
		b.WriteString("本章最接近的原文是：\n")
		for _, l := range cands {
			fmt.Fprintf(&b, "- %s\n", l)
		}
		b.WriteString("请逐字复制上面的一行（或它的一部分）作为 find，不要改写、不要用省略号代替中间内容。")
	} else {
		b.WriteString("请从本章正文里逐字复制一段连续原文作为 find。")
	}
	return fmt.Errorf("%s", b.String())
}

// shortAnchorRunes 是"短到几乎没有唯一性"的阈值。
//
// 它**不是**校验门槛：唯一性由计数判定，短锚点只要本章真的只出现一次就放行。
// 这个数字只在"已经不命中"之后用来解释原因，避免模型继续往更短的方向改。
const shortAnchorRunes = 6

// sentenceAround returns the sentence containing base[at], clipped for prompts.
func sentenceAround(base []rune, at, size int) string {
	lo := at
	for lo > 0 && !isSentenceEnd(base[lo-1]) && at-lo < 40 {
		lo--
	}
	hi := at + size
	for hi < len(base) && !isSentenceEnd(base[hi-1]) && hi-at < size+40 {
		hi++
	}
	return clipRunes(strings.TrimSpace(string(base[lo:hi])), 60)
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '\n':
		return true
	}
	return false
}

// lineOf reports the 1-based line number of rune offset idx.
func lineOf(base []rune, idx int) int {
	n := 1
	for i := 0; i < idx && i < len(base); i++ {
		if base[i] == '\n' {
			n++
		}
	}
	return n
}

// similarLines picks the lines of base most similar to find (Dice on bigrams).
//
// 中文短句用二元组重合度就够了：够快、不需要引入依赖，也不会像编辑距离那样
// 在长段落上给出误导性的"很像"。
func similarLines(base []rune, find string, want int) []string {
	lines := strings.Split(string(base), "\n")
	target := bigrams(find)
	if len(target) == 0 {
		return nil
	}
	type scored struct {
		line  string
		score float64
	}
	var all []scored
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		all = append(all, scored{line: line, score: dice(target, bigrams(line))})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	out := make([]string, 0, want)
	for _, s := range all {
		if len(out) >= want || s.score <= 0 {
			break
		}
		out = append(out, clipRunes(s.line, 60))
	}
	return out
}

func bigrams(s string) map[string]struct{} {
	r := []rune(strings.TrimSpace(s))
	m := make(map[string]struct{}, len(r))
	for i := 0; i+1 < len(r); i++ {
		m[string(r[i:i+2])] = struct{}{}
	}
	return m
}

func dice(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	common := 0
	for k := range a {
		if _, ok := b[k]; ok {
			common++
		}
	}
	return 2 * float64(common) / float64(len(a)+len(b))
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func runeLen(s string) int { return len([]rune(s)) }

// applyChapterEdits applies every edit to the original chapter body.
//
// 三个硬性约束，都是"错误可以回喂、改动不可猜"的体现：
//  1. 每条 find 必须在本章内唯一命中（0 处或多处都拒绝）。
//  2. edits 之间不得重叠：那是模型把同一段拆成相邻多条，合并才是正确写法。
//  3. 套用后必须真的改变了正文：否则作者点「确认落盘」等于什么都没发生。
//
// 插入（扩写）走两条路：借锚点的 replace，和不需要锚点的 append（章末）。
func applyChapterEdits(base string, edits []chapterEdit) (string, error) {
	if len(edits) == 0 {
		return "", fmt.Errorf("edits 为空：一次修订至少要给出一条改动；如果审校问题无需改字，请在审校页只勾选真正要改的条目")
	}
	baseRunes := []rune(base)
	spans := make([]locatedEdit, 0, len(edits))
	var appends []string
	for i, e := range edits {
		if e.isAppend() {
			if strings.TrimSpace(e.Append) == "" {
				return "", fmt.Errorf("edits[%d].append 只有空白：要追加内容就写出正文，不需要追加就不要输出这条 edit", i)
			}
			if e.Find != "" || e.Replace != "" {
				return "", fmt.Errorf("edits[%d] 同时给了 append 与 find/replace：两种形态只能用一种——章末追加用 append，改中间某处用 find+replace", i)
			}
			appends = append(appends, e.Append)
			continue
		}
		if strings.TrimSpace(e.Find) == "" {
			return "", fmt.Errorf("edits[%d].find 为空：改中间某处需要逐字复制被改动的一小段原文；要在章末加内容请改用 append", i)
		}
		if e.Find == e.Replace {
			return "", fmt.Errorf("edits[%d] 的 find 与 replace 完全相同，是空操作；没有改动就不要输出这条 edit", i)
		}
		span, err := locateEdit(baseRunes, e.Find, i)
		if err != nil {
			return "", err
		}
		span.replace = e.Replace
		spans = append(spans, span)
	}

	sort.SliceStable(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return "", fmt.Errorf(
				"edits[%d] 与 edits[%d] 在正文中重叠（第 %d 行附近）：同一段内的多处修改请合并成一条 edit，不要拆成相邻两条",
				spans[i-1].index, spans[i].index, lineOf(baseRunes, spans[i].start))
		}
	}

	// 顺序拼接而不是按下标倒序修改：没有偏移计算，也就没有偏移漂移。
	var b strings.Builder
	prev := 0
	for _, s := range spans {
		b.WriteString(string(baseRunes[prev:s.start]))
		b.WriteString(s.replace)
		prev = s.end
	}
	b.WriteString(string(baseRunes[prev:]))
	// 追加一定发生在所有替换之后：章末位置与它们无关，拼在末尾不会互相影响。
	for _, a := range appends {
		b.WriteString(a)
	}
	out := b.String()

	if out == base {
		return "", fmt.Errorf("补丁套用后与原章逐字相同：这些 edit 没有真正改变正文")
	}
	return out, nil
}

// parseChapterPatch enforces the patch contract and materializes the chapter.
//
// chapter 缺省（0）按本次调用的章号补齐，与审校结论的处理一致：调用范围已经
// 限定在这一章，"没写"不等于"写错了别的章"。写了别的章号才是矛盾，必须拒绝。
func parseChapterPatch(raw string, ch domain.ChapterDraft) (domain.ChapterDraft, error) {
	var parsed chapterPatch
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return domain.ChapterDraft{}, err
	}
	if parsed.Chapter == 0 {
		parsed.Chapter = ch.Chapter
	}
	if parsed.Chapter != ch.Chapter {
		return domain.ChapterDraft{}, contractFail("定向修订", []Violation{{
			Path: "chapter",
			Want: fmt.Sprintf("%d（本次只改这一章）", ch.Chapter),
			Got:  fmt.Sprint(parsed.Chapter),
		}})
	}
	body, err := applyChapterEdits(ch.Body, parsed.Edits)
	if err != nil {
		return domain.ChapterDraft{}, err
	}
	draft := domain.ChapterDraft{Chapter: parsed.Chapter, Title: strings.TrimSpace(parsed.Title), Body: body}
	if strings.TrimSpace(draft.Title) == "" {
		draft.Title = ch.Title
	}
	if err := validateRevisedChapter(draft, ch.Chapter); err != nil {
		return domain.ChapterDraft{}, err
	}
	return draft, nil
}
