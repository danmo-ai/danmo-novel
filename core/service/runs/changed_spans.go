package runs

import (
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ChangedSpans derives, per chapter, the sentences a revision actually touched.
//
// 为什么需要它：复审如果重审整章，会把「上几轮注意力没扫到的旧问题」当成
// 「修复引入的新问题」反复报出来。实测连续 6 轮复审里 96%+ 的新结论落在
// 这次根本没改过的原文上——清单因此永远收敛不了。
//
// 取「改动句 + 前后各一句」而不是只取改动句：修订常把矛盾修在相邻句里
// （改了调岗口径，下一句的称呼也得跟着变），只给改动的句子会让复审看不见
// 这类连带问题。前后各一句是这两者之间的折中。
//
// 未改动的章不出现在结果里：它们逐字未变，没有任何「新问题」可言。
func ChangedSpans(base, revised []domain.ChapterDraft) map[int][]string {
	if len(base) == 0 || len(revised) == 0 {
		return nil
	}
	oldByChapter := make(map[int]string, len(base))
	for _, ch := range base {
		oldByChapter[ch.Chapter] = ch.Body
	}
	out := map[int][]string{}
	for _, ch := range revised {
		old, ok := oldByChapter[ch.Chapter]
		if !ok {
			// 新增章：整章都算改动，但按句子给出去，让复审聚焦而不是盲审全章。
			if spans := sentences(ch.Body); len(spans) > 0 {
				out[ch.Chapter] = spans
			}
			continue
		}
		if old == ch.Body {
			continue
		}
		spans := changedSentences(old, ch.Body)
		if len(spans) > 0 {
			out[ch.Chapter] = spans
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sentences splits prose into sentence-ish units, keeping the terminator.
func sentences(text string) []string {
	var out []string
	var buf strings.Builder
	for _, r := range text {
		buf.WriteRune(r)
		switch r {
		case '。', '！', '？', '；', '\n':
			if s := strings.TrimSpace(buf.String()); s != "" {
				out = append(out, s)
			}
			buf.Reset()
		}
	}
	if s := strings.TrimSpace(buf.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// changedSentences returns changed sentences plus one neighbor on each side.
func changedSentences(oldBody, newBody string) []string {
	oldSet := map[string]bool{}
	for _, s := range sentences(oldBody) {
		oldSet[normalizeSentence(s)] = true
	}
	all := sentences(newBody)
	hit := make([]bool, len(all))
	for i, s := range all {
		if !oldSet[normalizeSentence(s)] {
			hit[i] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for i := range all {
		if !hit[i] {
			continue
		}
		for _, j := range []int{i - 1, i, i + 1} {
			if j < 0 || j >= len(all) {
				continue
			}
			s := strings.TrimSpace(all[j])
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// normalizeSentence collapses whitespace so that reformatting alone does not
// count as a change.
func normalizeSentence(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), "")
}
