package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 多轮审校取并集。
//
// 单轮审校的覆盖率有硬上限，这不是"审校不够深"，而是每轮注意力落点不同：
// 同一份正文跑 4 轮，并集 128 处里单轮平均只看到 25.4%（k=2 到 50.5%，
// k=3 到 75.4%），且 0 处被四轮全部抓到。提高 temperature 也没用——采样拿到的
// 是"另一批同样的真问题"，不是噪声。所以正确的做法是多轮采样再取并集，
// 而不是把单轮的提示词写得更狠。
//
// 代价出现在合并这一步：同一处缺陷，两轮的措辞可能毫无字面重合——
//
//	轮A  "车斗空着却在卸纸箱"   证据「车斗空着，两个人正从车上往下搬纸箱」
//	轮B  "拉货进度前后对不上"   证据 前天已装走两车半；周称昨天架子上满着
//
// 实测按 evidence 的字面重合去重，最多只能对上 19%。所以去重的键必须是
// **anchor 在正文里的区间**：同一个位置被两轮各自定位到，才是同一条问题。

// unionFindings merges findings from N review rounds over the same chapter.
//
// body 是本章正文，用来把 anchor 映射成区间。两者都定位成功且区间重叠 → 同一条。
// 定位失败时退到「章号 + 归一化 evidence」兜底键：宁可漏合并（作者多看一条近似
// 重复），也不要误合并（把两个不同缺陷并成一条，修了一个漏一个）。
func unionFindings(rounds [][]domain.ReviewFinding, body string) []domain.ReviewFinding {
	baseRunes := []rune(body)

	type entry struct {
		f       domain.ReviewFinding
		start   int // -1 = 没有可定位的锚点
		end     int
		anchors map[string]bool
		evidKey map[string]bool
	}
	var entries []*entry

	for _, round := range rounds {
		for _, f := range round {
			start, end := anchorSpan(baseRunes, f.Anchor)
			evKey := findingEvidenceKey(f)
			merged := false
			for _, e := range entries {
				if sameDefect(e.start, e.end, e.anchors, e.evidKey, start, end, f.Anchor, evKey) {
					mergeFinding(&e.f, f)
					if f.Anchor != "" {
						e.anchors[f.Anchor] = true
					}
					if evKey != "" {
						e.evidKey[evKey] = true
					}
					merged = true
					break
				}
			}
			if merged {
				continue
			}
			// 第一轮看到就算命中一次；后续轮合并进来才 +1。
			f.RoundsHit = 1
			e := &entry{f: f, start: start, end: end,
				anchors: map[string]bool{}, evidKey: map[string]bool{}}
			if evKey != "" {
				f.ID = fmt.Sprintf("%s-%s", f.Lens, evKey)
				e.f.ID = f.ID
			}
			if f.Anchor != "" {
				e.anchors[f.Anchor] = true
			}
			if evKey != "" {
				e.evidKey[evKey] = true
			}
			entries = append(entries, e)
		}
	}

	out := make([]domain.ReviewFinding, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.f)
	}
	return out
}

// anchorSpan maps a verbatim anchor onto a rune span in the chapter body.
//
// 复用补丁路径的四层匹配（逐字 → 行尾空白 → 首尾空白 → 标点归一），因为
// anchor 与补丁的 find 是同一类输入：模型抄中文时最常走样的是全角/半角标点
// 与行尾空白，而这几种差异不该让一条真问题在去重时被当成新的。
// 找不到返回 (-1, -1)，不做模糊匹配——猜错的代价是把两个缺陷并成一个。
func anchorSpan(base []rune, anchor string) (int, int) {
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return -1, -1
	}
	span, err := locateEdit(base, anchor, 0)
	if err != nil {
		return -1, -1
	}
	return span.start, span.end
}

// sameDefect reports whether two findings describe the same problem.
//
// 判定顺序（从可靠到宽松）：
//  1. 两条锚点都在正文里定位成功：区间重叠即同一处。
//  2. 一边定位成功、另一边锚点文本与已知锚点字面相同（归一化后）：同一处。
//  3. 都没有可用锚点：用「归一化 evidence 前缀」兜底，只认同一个键。
func sameDefect(aStart, aEnd int, aAnchors, aEvid map[string]bool, bStart, bEnd int, bAnchor, bEvid string) bool {
	if aStart >= 0 && bStart >= 0 {
		return aStart < bEnd && bStart < aEnd
	}
	if bAnchor != "" {
		if aAnchors[bAnchor] {
			return true
		}
	}
	if bEvid != "" && aEvid[bEvid] {
		return true
	}
	return false
}

// mergeFinding folds src into dst.
//
// severity 取最严：三轮里只要有一轮判 blocking，就按 blocking 处理——漏掉一条
// 硬伤的代价远大于多看一条告警。文案拼接去重，让作者看到各轮的措辞差异。
func mergeFinding(dst *domain.ReviewFinding, src domain.ReviewFinding) {
	if severityRank(src.Severity) > severityRank(dst.Severity) {
		dst.Severity = src.Severity
	}
	dst.RoundsHit++
	// 锚点保留第一条可用的：它已经成功定位过，换成别的说法不增加信息。
	if dst.Anchor == "" && src.Anchor != "" {
		dst.Anchor = src.Anchor
	}
	dst.Title = shorterNonEmpty(dst.Title, src.Title)
	dst.Evidence = joinDistinct(dst.Evidence, src.Evidence)
	dst.Suggestion = joinDistinct(dst.Suggestion, src.Suggestion)
	if dst.Lens == "" {
		dst.Lens = src.Lens
	}
}

func severityRank(s string) int {
	switch s {
	case "blocking":
		return 2
	case "advisory":
		return 1
	}
	return 0
}

// shorterNonEmpty keeps the terser title: 多轮里更短的那条通常更聚焦。
func shorterNonEmpty(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if len([]rune(b)) < len([]rune(a)) {
		return b
	}
	return a
}

// joinDistinct appends b to a unless a already contains it.
//
// 用「包含」而不是「相等」：第二轮常把第一轮的措辞扩展一句，合并后读起来
// 仍是一句话，而完全去等会留下一串近似重复的句子。
func joinDistinct(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if b == "" {
		return a
	}
	if a == "" {
		return b
	}
	if strings.Contains(a, b) {
		return a
	}
	if strings.Contains(b, a) {
		return b
	}
	return a + "；" + b
}

// findingEvidenceKey is the fallback dedup key when no anchor is usable.
//
// 只看前 20 字并去掉空白与标点：evidence 允许转述，但它开头那句往往就是
// 模型认定的问题所在，足以判"同一条意见被两轮各说了一遍"。
func findingEvidenceKey(f domain.ReviewFinding) string {
	raw := f.Evidence
	if strings.TrimSpace(raw) == "" {
		raw = f.Title
	}
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(raw) {
		if r == ' ' || r == '\n' || r == '\t' || r == '　' {
			continue
		}
		if strings.ContainsRune("「」『』“”\"'，。、；：？！,.;:?!", r) {
			continue
		}
		b.WriteRune(r)
		n++
		if n >= 20 {
			break
		}
	}
	if n < 4 {
		return "" // 太短，当键会误合并
	}
	return fmt.Sprintf("ch%d:%s", f.Chapter, b.String())
}

// rankFindings orders the union so the author reads the strongest signal first.
//
// 3 轮并集约是单轮的 2–3 倍条数。不排序就是灾难：作者面对翻倍的问题清单，
// 无法判断哪些值得修。实测被 ≥2 轮独立检出的只占约 20%，那批才是可复现的
// 硬伤；单轮命中的更可能是这一轮的注意力偏好。
func rankFindings(findings []domain.ReviewFinding) []domain.ReviewFinding {
	out := append([]domain.ReviewFinding(nil), findings...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		// 1. 复现次数多的优先（共识硬伤）。
		if a.RoundsHit != b.RoundsHit {
			return a.RoundsHit > b.RoundsHit
		}
		// 2. blocking 优先。
		if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
			return ra > rb
		}
		// 3. 同档按章号，保证同一章的条目相邻，作者能一次看完。
		return a.Chapter < b.Chapter
	})
	return out
}
