package gates

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

var (
	emDashRe     = regexp.MustCompile(`[—–]`)
	aiVocabRe    = regexp.MustCompile(`(?:不禁|目光深邃|心中一动|涌上心头|嘴角微微上扬|空气仿佛凝固|仿佛时间静止)`)
	englishLeak  = regexp.MustCompile(`[A-Za-z]{4,}`)
	simileHeavy  = regexp.MustCompile(`如同|宛如|仿佛|好似`)
)

type Report struct {
	OK       bool                  `json:"ok"`
	Findings []domain.ReviewFinding `json:"findings"`
	Counts   map[string]int        `json:"counts"`
}

func CheckProse(chapters []domain.ChapterDraft, wordTargetPerChapter int) Report {
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
		if wordTargetPerChapter > 0 && chars < int(float64(wordTargetPerChapter)*0.6) {
			r.OK = false
			r.Findings = append(r.Findings, domain.ReviewFinding{
				ID: fmt.Sprintf("length-ch%d", ch.Chapter), Severity: "blocking", Source: "local_gate",
				Lens: "length", Title: "字数不足", Chapter: ch.Chapter,
				Evidence: fmt.Sprintf("chars=%d target≈%d", chars, wordTargetPerChapter),
				Suggestion: "扩写至目标字数区间",
			})
		}
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
	}
	r.Counts["em_dash"] = totalEm
	r.Counts["ai_vocab"] = totalAI
	r.Counts["english_leak"] = totalEN
	r.Counts["simile"] = totalSim
	return r
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
