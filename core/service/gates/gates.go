package gates

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 规则表已移出代码：见 core/resource/novel/deslop-rules.yaml 与 rules.go。
// 加词、调阈值、开关某条都是编辑 YAML，不需要改 Go，也不需要重新发版。

var (
	aiVocabRe   = regexp.MustCompile(`(?:不禁|目光深邃|心中一动|涌上心头|嘴角微微上扬|空气仿佛凝固|仿佛时间静止)`)
	emDashRe    = regexp.MustCompile(`[—–]`)
	englishLeak = regexp.MustCompile(`[A-Za-z]{4,}`)
	simileHeavy = regexp.MustCompile(`如同|宛如|仿佛|好似`)
)

type Report struct {
	OK       bool                   `json:"ok"`
	Findings []domain.ReviewFinding `json:"findings"`
	Counts   map[string]int         `json:"counts"`
}

// CheckProse runs the fixed structural checks plus the built-in deslop rules.
func CheckProse(chapters []domain.ChapterDraft, chapterTarget int) Report {
	return CheckProseWithRules(chapters, chapterTarget, nil)
}

// CheckProseWithRules is CheckProse with an author-resolved rule set.
// The pipeline passes the book/library one; nil falls back to the embedded set.
//
// chapterTarget 必须来自 books.ChapterTarget——它是「每章多少字」的唯一读数，
// 硬门不再自己算目标，也不认字面量。目标与接受区间都在 domain 里定义。
func CheckProseWithRules(chapters []domain.ChapterDraft, chapterTarget int, rules *Ruleset) Report {
	if rules == nil {
		rules = BuiltinRules()
	}
	r := Report{OK: true, Counts: map[string]int{}}
	totalEm, totalAI, totalEN, totalSim := 0, 0, 0, 0
	for _, ch := range chapters {
		body := ch.Body
		em := len(emDashRe.FindAllString(body, -1))
		ai := len(aiVocabRe.FindAllString(body, -1))
		en := 0
		for _, m := range englishLeak.FindAllString(body, -1) {
			// ignore short romanizations in names by rough heuristic
			if len(m) >= 5 {
				en++
			}
		}
		sim := len(simileHeavy.FindAllString(body, -1))
		totalEm += em
		totalAI += ai
		totalEN += en
		totalSim += sim
		chars := utf8.RuneCountInString(strings.ReplaceAll(body, " ", ""))
		if chapterTarget > 0 {
			floor, ceil := domain.ChapterBand(chapterTarget)
			if chars < floor {
				r.OK = false
				r.Findings = append(r.Findings, domain.ReviewFinding{
					ID: fmt.Sprintf("length-ch%d", ch.Chapter), Severity: "blocking", Source: "local_gate",
					Lens: "length", Title: "字数不足", Chapter: ch.Chapter,
					Evidence:   fmt.Sprintf("chars=%d target≈%d floor=%d", chars, chapterTarget, floor),
					Suggestion: "扩写至目标字数区间",
				})
			} else if chars > ceil {
				// 上限曾经完全没判：写多长都算通过，单章可以涨到目标的 1.4 倍以上，
				// 单元总量随之失控。先按 advisory 报，不新增阻断面。
				r.Findings = append(r.Findings, domain.ReviewFinding{
					ID: fmt.Sprintf("length-over-ch%d", ch.Chapter), Severity: "advisory", Source: "local_gate",
					Lens: "length", Title: "字数超出目标区间", Chapter: ch.Chapter,
					Evidence:   fmt.Sprintf("chars=%d target≈%d ceil=%d", chars, chapterTarget, ceil),
					Suggestion: "删冗余节拍或拆章；别靠删结尾的钩子凑字数",
				})
			}
		}
		// 以下旧阈值保持不变（只加不减，避免已有书突然变宽松）。
		if em > 8 {
			r.OK = false
			r.Findings = append(r.Findings, domain.ReviewFinding{
				ID: fmt.Sprintf("emdash-ch%d", ch.Chapter), Severity: "blocking", Source: "local_gate",
				Lens: "deslop", Title: "破折号过多", Chapter: ch.Chapter,
				Evidence: fmt.Sprintf("em_dash=%d", em),
			})
		}
		if en > 3 {
			r.OK = false
			r.Findings = append(r.Findings, domain.ReviewFinding{
				ID: fmt.Sprintf("en-ch%d", ch.Chapter), Severity: "blocking", Source: "local_gate",
				Lens: "deslop", Title: "英文泄漏", Chapter: ch.Chapter,
				Evidence: fmt.Sprintf("english_leak=%d", en),
			})
		}
		if ai > 4 {
			r.Findings = append(r.Findings, domain.ReviewFinding{
				ID: fmt.Sprintf("ai-ch%d", ch.Chapter), Severity: "advisory", Source: "local_gate",
				Lens: "deslop", Title: "AI 味词汇偏多", Chapter: ch.Chapter,
				Evidence: fmt.Sprintf("ai_vocab=%d", ai),
			})
		}
		if sim > 8 {
			r.Findings = append(r.Findings, domain.ReviewFinding{
				ID: fmt.Sprintf("simile-ch%d", ch.Chapter), Severity: "advisory", Source: "local_gate",
				Lens: "deslop", Title: "比喻词偏多", Chapter: ch.Chapter,
				Evidence:   fmt.Sprintf("simile=%d (如同/宛如/仿佛/好似)", sim),
				Suggestion: "减少比喻密度，保留最贴切的一两个",
			})
		}

		for _, f := range checkProseSignals(rules, ch, chars) {
			if f.Severity == "blocking" {
				r.OK = false
			}
			r.Findings = append(r.Findings, f)
		}
	}
	r.Counts["em_dash"] = totalEm
	r.Counts["ai_vocab"] = totalAI
	r.Counts["english_leak"] = totalEN
	r.Counts["simile"] = totalSim
	return r
}

// checkProseSignals runs the 去 AI 味 checks for one chapter using the
// author-resolved rule set (see deslop-rules.yaml). Adding or tuning a rule is
// a YAML edit, never a code change.
//
// 两条设计约束（来自社区实测的教训）：
//  1. 按每千字密度判定，不按绝对条数——否则短章被小样本放大、长章永远漏报。
//  2. 聚簇判定：单个弱信号不定罪（真人也会用破折号和排比），只有多项同时密集
//     才补一条「成簇」提示。避免把作者自己的文风当成 AI 味删掉。
func checkProseSignals(rs *Ruleset, ch domain.ChapterDraft, chars int) []domain.ReviewFinding {
	if chars <= 0 || rs == nil {
		return nil
	}
	var out []domain.ReviewFinding
	// 每千字基数：短章用 1000 兜底，避免 200 字段落算出一万/千字的荒谬密度。
	perKilo := float64(chars) / 1000
	if perKilo < 1 {
		perKilo = 1
	}

	hitsByCategory := 0
	weak := 0
	for _, sig := range rs.Signals {
		hits := sig.hits(ch.Body)
		if hits == 0 {
			continue
		}
		if sig.MinHits > 0 && hits < sig.MinHits {
			// 未过绝对下限；密度极高时仍算一类命中（长章的成簇判定）。
			if sig.PerMille <= 0 || float64(hits)/perKilo < sig.PerMille {
				continue
			}
		}
		if sig.PerMille > 0 && float64(hits)/perKilo < sig.PerMille {
			continue
		}
		hitsByCategory++
		if sig.Severity == SeverityWeak {
			weak++
			continue
		}
		f := domain.ReviewFinding{
			ID:       fmt.Sprintf("%s-ch%d", sig.ID, ch.Chapter),
			Severity: sig.Severity, Source: "local_gate", Lens: sig.Lens,
			Title: sig.Title, Chapter: ch.Chapter,
			Evidence:   fmt.Sprintf("%d 处，密度 %.1f/千字", hits, float64(hits)/perKilo),
			Suggestion: sig.Suggest,
		}
		out = append(out, f)
	}

	// 成簇：多类同时命中，说明不是单点口味问题，而是整章的 AI 腔。
	if hitsByCategory >= rs.ClusterAt {
		out = append(out, domain.ReviewFinding{
			ID:       fmt.Sprintf("ai-cluster-ch%d", ch.Chapter),
			Severity: "advisory", Source: "local_gate", Lens: "deslop",
			Title: "成簇的 AI 文风特征", Chapter: ch.Chapter,
			Evidence:   fmt.Sprintf("%d 类特征同时命中（含弱信号 %d 类）", hitsByCategory, weak),
			Suggestion: "单条特征不算证据，成簇说明整章腔调有问题：优先改叙述者的态度与具体细节，别逐词替换",
		})
	}
	return out
}
func CheckUnitOutline(u domain.UnitOutlineDraft, volumeCast []string) Report {
	r := Report{OK: true, Counts: map[string]int{}}
	if u.UnitID == "" {
		r.OK = false
		r.Findings = append(r.Findings, finding("outline-id", "blocking", "缺少 unitId"))
	}
	if u.Function == "" || u.Desire == "" || u.Obstacle == "" || u.Choice == "" {
		r.OK = false
		r.Findings = append(r.Findings, finding("outline-contract", "blocking", "细纲合同字段不完整（function/desire/obstacle/choice）"))
	}
	if len(u.OnStage) == 0 {
		r.OK = false
		r.Findings = append(r.Findings, finding("outline-onstage", "blocking", "on_stage 为空"))
	}
	if u.POV == "" {
		r.OK = false
		r.Findings = append(r.Findings, finding("outline-pov", "blocking", "pov 为空"))
	} else {
		found := false
		for _, s := range u.OnStage {
			if s == u.POV {
				found = true
				break
			}
		}
		if !found {
			r.OK = false
			r.Findings = append(r.Findings, finding("outline-pov-stage", "blocking", "pov 必须属于 on_stage"))
		}
	}
	if len(volumeCast) > 0 {
		allowed := map[string]bool{}
		for _, s := range volumeCast {
			allowed[s] = true
		}
		for _, s := range u.OnStage {
			if !allowed[s] {
				// 新 stem 不 blocking：单元案受害者/嫌疑人可临时引入，
				// 但会以 candidate 播种，作者晋升 canon 后才算正式人物。
				r.Findings = append(r.Findings, finding("outline-new-cast-"+s, "advisory", "新人物待晋升 canon: "+s+"（接受细纲后将播种 candidate 人物卡，请在设定页补全并晋升）"))
			}
		}
	}
	if u.ChapterFrom <= 0 || u.ChapterTo < u.ChapterFrom || (u.ChapterTo-u.ChapterFrom+1) > 10 {
		r.OK = false
		r.Findings = append(r.Findings, finding("outline-range", "blocking", "章范围非法或超过 10 章硬上限"))
		return r
	}
	// 字数额度：单章字数只有一本书级的定义（domain.ChapterChars*），细纲不能
	// 自己发明。典型事故是 word_target: 30000 / 6 章 —— 读出来就是每章 5000 字，
	// 与知识库的 2000–3500 打架，写作硬门把整单元拦下。这里在细纲阶段就拦住，
	// 让模型用一次修复把数字改成合法的，而不是等写完正文才发现。
	chapters := u.ChapterTo - u.ChapterFrom + 1
	if cc, err := strconv.Atoi(strings.Trim(strings.TrimSpace(u.ChapterChars), `"'`)); err == nil && cc > 0 {
		if cc < domain.ChapterCharsMin || cc > domain.ChapterCharsMax {
			r.OK = false
			r.Findings = append(r.Findings, finding("outline-chapter-chars", "blocking",
				fmt.Sprintf("chapter_chars=%d 超出单章区间 %d–%d 字", cc, domain.ChapterCharsMin, domain.ChapterCharsMax)))
		}
	}
	if wt, err := strconv.Atoi(strings.Trim(strings.TrimSpace(u.WordTarget), `"'`)); err == nil && wt > 0 {
		target := domain.ClampChapterChars(wt / chapters)
		// 允许每章 1 字的取整误差：老书回写的是实测值（如 21274/10）。
		if diff := wt - target*chapters; diff > chapters || diff < -chapters {
			r.OK = false
			r.Findings = append(r.Findings, finding("outline-word-target", "blocking",
				fmt.Sprintf("word_target=%d 折合每章 %d 字，超出 %d–%d 字区间；%d 章应写 %d–%d（或改用 chapter_chars）",
					wt, wt/chapters, domain.ChapterCharsMin, domain.ChapterCharsMax,
					chapters, domain.ChapterCharsMin*chapters, domain.ChapterCharsMax*chapters)))
		}
	}
	return r
}

func CheckCast(cards []domain.CastCard) Report {
	r := Report{OK: true, Counts: map[string]int{"cast": len(cards)}}
	if len(cards) == 0 {
		r.OK = false
		r.Findings = append(r.Findings, finding("cast-empty", "blocking", "无人物卡"))
		return r
	}
	hasProtagonist := false
	for _, c := range cards {
		if c.Role == "protagonist" && c.Status == domain.CastCanon {
			hasProtagonist = true
		}
		if c.Role == "" {
			r.Findings = append(r.Findings, finding("cast-role-"+c.Stem, "blocking", "缺少 role: "+c.Stem))
			r.OK = false
		}
	}
	if !hasProtagonist {
		r.OK = false
		r.Findings = append(r.Findings, finding("cast-protagonist", "blocking", "缺少 canon 主角"))
	}
	return r
}

func finding(id, sev, title string) domain.ReviewFinding {
	return domain.ReviewFinding{ID: id, Severity: sev, Source: "local_gate", Title: title}
}

// ParseProseChapters splits a unit markdown file.
func ParseProseChapters(md string) []domain.ChapterDraft {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out []domain.ChapterDraft
	var cur *domain.ChapterDraft
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(strings.Join(body, "\n"))
		out = append(out, *cur)
		cur = nil
		body = nil
	}
	re := regexp.MustCompile(`^## 第(\d+)章(?:\s+(.*))?$`)
	for _, line := range lines {
		if m := re.FindStringSubmatch(line); m != nil {
			flush()
			n, _ := strconv.Atoi(m[1])
			cur = &domain.ChapterDraft{Chapter: n, Title: strings.TrimSpace(m[2])}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.TrimSpace(line) == "---" {
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}
