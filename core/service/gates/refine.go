package gates

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// refineShingle is the n-gram width used to measure how much of the old chapter
// survived an optimize-mode rewrite. 8 个汉字足够长到不会被常用短语刷出假重合，
// 又短到能反映"句子换了说法但内容仍是原句"的改写。
const refineShingle = 8

// 两个阈值都刻意留得很松：advisory 只在"几乎没改"和"几乎全改"两头提醒，
// 中间那段（正常优化，通常保留 50%–85% 的 8-gram）不该打扰作者。
const (
	refineKeepTooHigh = 0.92 // 低于这个不改：超过说明基本是照抄/同义词替换
	refineKeepTooLow  = 0.35 // 高于这个不改：低于说明这次更像重写而不是优化
)

// RefineSimilarityFindings reports how much an optimize-mode rewrite actually
// changed, so the author can tell whether the mode fit the intent.
//
// 这不是质量判定：改动幅度大不等于写得差。它判的是"这次调用有没有兑现作者
// 选的那个模式"——选「在原文上优化」却拿回一篇完全不同的稿，或者拿回原文的
// 同义词版，两种都需要作者重新决定，而不是直接接受。
//
// 只产出 advisory（Severity=advisory），绝不阻断接受：正文好坏仍由审校与作者判断。
func RefineSimilarityFindings(original, revised []domain.ChapterDraft) []domain.ReviewFinding {
	if len(original) == 0 || len(revised) == 0 {
		return nil
	}
	byChapter := make(map[int]domain.ChapterDraft, len(original))
	for _, ch := range original {
		byChapter[ch.Chapter] = ch
	}
	var out []domain.ReviewFinding
	for _, ch := range revised {
		old, ok := byChapter[ch.Chapter]
		if !ok {
			continue // 新增章没有基线，比不了
		}
		keep, ok := shingleCoverage(old.Body, ch.Body)
		if !ok {
			continue
		}
		pct := int(keep*100 + 0.5)
		switch {
		case keep >= refineKeepTooHigh:
			out = append(out, domain.ReviewFinding{
				ID:       fmt.Sprintf("refine-keep-%d", ch.Chapter),
				Severity: "advisory",
				Source:   "local_gate",
				Lens:     "refine",
				Chapter:  ch.Chapter,
				Title:    fmt.Sprintf("第 %d 章与原文重合 %d%%，可能只是同义词替换", ch.Chapter, pct),
				Evidence: "本次选择的是「在原文上优化」，但正文骨架几乎逐句沿用原稿。若只想修个别问题，用审校后的「自动修复」更省；若确实只想微调表达，可以忽略这条。",
			})
		case keep <= refineKeepTooLow:
			out = append(out, domain.ReviewFinding{
				ID:       fmt.Sprintf("refine-rewrite-%d", ch.Chapter),
				Severity: "advisory",
				Source:   "local_gate",
				Lens:     "refine",
				Chapter:  ch.Chapter,
				Title:    fmt.Sprintf("第 %d 章只保留了原文 %d%%，这次更像重写", ch.Chapter, pct),
				Evidence: "「在原文上优化」要求保留场次与事件骨架。若这次本来就想换写法，改用不带旧稿的「重新生成」更合适；否则请核对事件与顺序有没有被改掉。",
			})
		}
	}
	return out
}

// shingleCoverage returns the share of the new chapter's 8-gram shingles that
// already existed in the old chapter. 1 = 一字未改，0 = 全新内容。
//
// 空白与换行先折叠掉：排版变动不该被算成"改写"。标点保留——标点本身就是
// 节奏，全换标点的稿子值得被判成改动。
func shingleCoverage(oldBody, newBody string) (float64, bool) {
	oldSet := shingleSet(oldBody)
	if len(oldSet) == 0 {
		return 0, false
	}
	newSet := shingleSet(newBody)
	if len(newSet) == 0 {
		return 0, false
	}
	hit := 0
	for s := range newSet {
		if _, ok := oldSet[s]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(newSet)), true
}

func shingleSet(body string) map[string]struct{} {
	r := []rune(strings.Join(strings.Fields(body), ""))
	if len(r) < refineShingle {
		return nil
	}
	set := make(map[string]struct{}, len(r)-refineShingle+1)
	for i := 0; i+refineShingle <= len(r); i++ {
		set[string(r[i:i+refineShingle])] = struct{}{}
	}
	return set
}
