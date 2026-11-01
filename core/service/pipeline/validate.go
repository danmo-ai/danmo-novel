package pipeline

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// Structured-output contracts.
//
// 每个任务的 system prompt 都写了输出契约，但契约只是"说过"；模型不遵守时，
// 缺字段会被零值静默吸收（severity="" → 当成 advisory、chapter=0 → 排序错位、
// 漏章 → 正文与细纲/摘要/账本全部错位）。这一层把契约变成可执行的检查：
// 校验失败 → 错误文本喂回模型（MergeRepairIntoUserPayload）→ 重试一次。

const (
	severityBlocking = "blocking"
	severityAdvisory = "advisory"
	verdictPass      = "pass"
	verdictRevise    = "revise"
)

// Violation is one failed field requirement.
//
// 带路径与期望值，"格式不对"这种话模型改不动，
// "findings[2].severity：需要 blocking 或 advisory，实际 fatal" 它才改得动。
type Violation struct {
	Path string
	Want string
	Got  string
}

func (v Violation) String() string {
	got := strings.TrimSpace(v.Got)
	if got == "" {
		got = "（空）"
	}
	return fmt.Sprintf("%s：需要 %s，实际 %s", v.Path, v.Want, got)
}

// ContractError aggregates every violation of one response.
//
// 聚合而不是遇到第一个就返回：模型一次能看到的错误越多，一次重试能修完的概率
// 越高。逐条返回会让两处错误变成两轮重试，把固定预算烧光。
type ContractError struct {
	Task       string
	Violations []Violation
}

func (e *ContractError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s 输出结构不合规（%d 处）：\n", e.Task, len(e.Violations))
	for i, v := range e.Violations {
		fmt.Fprintf(&b, "%d. %s\n", i+1, v)
	}
	b.WriteString("请按契约修正后重新输出完整 JSON；已经合规的部分原样保留。")
	return b.String()
}

func contractFail(task string, vs []Violation) error {
	if len(vs) == 0 {
		return nil
	}
	return &ContractError{Task: task, Violations: vs}
}

// validateChapters enforces the write contract.
//
// 章号与章体是下游一切计算的基础：章节排序、摘要回写、账本游标、门禁统计。
// 漏章不会报错，只会让下次续写从头开始错位，所以这里必须拦住。
func validateChapters(task string, chapters []domain.ChapterDraft, from, to int) error {
	if len(chapters) == 0 {
		return contractFail(task, []Violation{{Path: "chapters", Want: "至少一章", Got: "0 章"}})
	}
	var vs []Violation
	seen := map[int]bool{}
	for i, ch := range chapters {
		if ch.Chapter <= 0 {
			vs = append(vs, Violation{
				Path: fmt.Sprintf("chapters[%d].chapter", i),
				Want: "正整数章号",
				Got:  fmt.Sprint(ch.Chapter),
			})
			continue
		}
		if seen[ch.Chapter] {
			vs = append(vs, Violation{
				Path: fmt.Sprintf("chapters[%d].chapter", i),
				Want: "章号唯一",
				Got:  fmt.Sprintf("第 %d 章重复出现", ch.Chapter),
			})
		}
		seen[ch.Chapter] = true
		if strings.TrimSpace(ch.Body) == "" {
			vs = append(vs, Violation{
				Path: fmt.Sprintf("chapters[%d].body", i),
				Want: "非空正文",
				Got:  fmt.Sprintf("第 %d 章正文为空", ch.Chapter),
			})
		}
	}
	if from > 0 && to >= from {
		if missing := rangeMissing(seen, from, to); len(missing) > 0 {
			vs = append(vs, Violation{
				Path: "chapters[].chapter",
				Want: fmt.Sprintf("完整覆盖第 %d–%d 章", from, to),
				Got:  "缺少第 " + joinInts(missing) + " 章",
			})
		}
		if extra := rangeOutside(seen, from, to); len(extra) > 0 {
			vs = append(vs, Violation{
				Path: "chapters[].chapter",
				Want: fmt.Sprintf("不超出第 %d–%d 章", from, to),
				Got:  "多出第 " + joinInts(extra) + " 章",
			})
		}
	}
	return contractFail(task, vs)
}

// validateReviewFindings enforces the review contract.
//
// severity 是下游唯一的开关（chain 是否阻断、定稿是否放行），所以未知值不能
// 静默降级成 advisory——那会把一条真硬伤藏起来。id 为空则去重与「按 issueId
// 修订」都会失效。wantChapter>0 时校验章号：审校是按章调用的，报别的章说明
// 模型没在读本章。
func validateReviewFindings(findings []domain.ReviewFinding, wantChapter int) error {
	var vs []Violation
	for i, f := range findings {
		base := fmt.Sprintf("findings[%d]", i)
		if strings.TrimSpace(f.ID) == "" {
			vs = append(vs, Violation{Path: base + ".id", Want: "非空，格式 <lens>-<章>-<序号>", Got: ""})
		}
		switch f.Severity {
		case severityBlocking, severityAdvisory:
		default:
			vs = append(vs, Violation{Path: base + ".severity", Want: "blocking 或 advisory", Got: f.Severity})
		}
		if strings.TrimSpace(f.Title) == "" {
			vs = append(vs, Violation{Path: base + ".title", Want: "≤20 字的问题标题", Got: ""})
		}
		if strings.TrimSpace(f.Evidence) == "" {
			vs = append(vs, Violation{Path: base + ".evidence", Want: "≤40 字，只摘关键原文或点明事实", Got: ""})
		}
		if wantChapter > 0 && f.Chapter > 0 && f.Chapter != wantChapter {
			vs = append(vs, Violation{
				Path: base + ".chapter",
				Want: fmt.Sprintf("第 %d 章（本次只审这一章）", wantChapter),
				Got:  fmt.Sprintf("第 %d 章", f.Chapter),
			})
		}
	}
	return contractFail("审校", vs)
}

// validateVerdict enforces the verdict enum.
func validateVerdict(verdict string) error {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case verdictPass, verdictRevise:
		return nil
	}
	return contractFail("审校", []Violation{{Path: "verdict", Want: "pass 或 revise", Got: verdict}})
}

// validateRevisedChapter enforces the targeted-revision contract.
//
// title 不在此列：调用方会沿用原标题，缺标题不构成数据损失。
func validateRevisedChapter(ch domain.ChapterDraft, want int) error {
	var vs []Violation
	if ch.Chapter != want {
		vs = append(vs, Violation{
			Path: "chapter",
			Want: fmt.Sprintf("%d（本次只改这一章）", want),
			Got:  fmt.Sprint(ch.Chapter),
		})
	}
	if strings.TrimSpace(ch.Body) == "" {
		vs = append(vs, Violation{Path: "body", Want: "修订后的完整正文", Got: ""})
	}
	return contractFail("定向修订", vs)
}

// validateOutlineScenes checks that scenes are placed inside their own unit's
// chapter range and that every chapter gets at least one scene.
//
// 越界的 scene 会让「本章场面节拍」的按章过滤失效：那一章拿不到任何节拍，
// 写作时等于盲写。缺章的 scene 更隐蔽——细纲看起来完整，写作却无米下锅。
func validateOutlineScenes(units []domain.UnitOutlineDraft) error {
	var vs []Violation
	for i, u := range units {
		if u.ChapterFrom <= 0 || u.ChapterTo < u.ChapterFrom {
			continue // 章范围本身非法，由 CheckUnitOutline 报
		}
		if len(u.Scenes) == 0 {
			continue // 缺 scenes 由 CheckUnitOutline 报
		}
		covered := map[int]bool{}
		for j, sc := range u.Scenes {
			n := sceneChapter(sc)
			if n == 0 {
				vs = append(vs, Violation{
					Path: fmt.Sprintf("units[%d].scenes[%d].chapter", i, j),
					Want: "数字章号",
					Got:  fmt.Sprint(sc["chapter"]),
				})
				continue
			}
			if n < u.ChapterFrom || n > u.ChapterTo {
				vs = append(vs, Violation{
					Path: fmt.Sprintf("units[%d].scenes[%d].chapter", i, j),
					Want: fmt.Sprintf("落在本单元第 %d–%d 章内", u.ChapterFrom, u.ChapterTo),
					Got:  fmt.Sprintf("第 %d 章", n),
				})
				continue
			}
			covered[n] = true
		}
		if missing := rangeMissing(covered, u.ChapterFrom, u.ChapterTo); len(missing) > 0 {
			vs = append(vs, Violation{
				Path: fmt.Sprintf("units[%d].scenes", i),
				Want: "每章至少一个 scene",
				Got:  "缺第 " + joinInts(missing) + " 章的 scene",
			})
		}
	}
	return contractFail("细纲", vs)
}

// normalizePlanCast applies the rule the plan prompt already states: during
// 立项 every card is a candidate. An omitted status is therefore not missing
// data, it is the stated value; an explicit non-candidate value is still an
// error, because promoting to canon is the author's decision.
func normalizePlanCast(rows []map[string]any) {
	for _, row := range rows {
		if row == nil {
			continue
		}
		if v, ok := row["status"]; !ok || v == nil || strings.TrimSpace(fmt.Sprint(v)) == "" {
			row["status"] = "candidate"
		}
	}
}

// validatePlanCast enforces the cast contract from the 立项 prompt:
// stem/name/role/status/cardMarkdown are required, exactly one protagonist,
// and every card starts with `# 纯姓名`.
//
// 立项一次生成整本书的人物骨架；这里缺一个 stem，后面每一张人物卡、每一次
// on_stage 引用都会落空，而错误会拖到写作阶段才显形。
func validatePlanCast(rows []map[string]any) error {
	if len(rows) == 0 {
		return contractFail("立项", []Violation{{Path: "cast", Want: "至少一张人物卡", Got: "0 张"}})
	}
	var vs []Violation
	protagonists := 0
	for i, row := range rows {
		base := fmt.Sprintf("cast[%d]", i)
		field := func(k string) string {
			if v, ok := row[k]; ok && v != nil {
				return strings.TrimSpace(fmt.Sprint(v))
			}
			return ""
		}
		stem := field("stem")
		if stem == "" {
			vs = append(vs, Violation{Path: base + ".stem", Want: "英文小写连字符", Got: ""})
		} else if strings.ContainsAny(stem, " \t/\\") || stem != strings.ToLower(stem) {
			vs = append(vs, Violation{Path: base + ".stem", Want: "英文小写连字符，不含空格与路径符", Got: stem})
		}
		if field("name") == "" {
			vs = append(vs, Violation{Path: base + ".name", Want: "纯姓名，不含括号说明", Got: ""})
		}
		role := strings.ToLower(field("role"))
		switch role {
		case "protagonist", "volume_antagonist", "recurring":
		default:
			vs = append(vs, Violation{Path: base + ".role", Want: "protagonist / volume_antagonist / recurring", Got: field("role")})
		}
		if role == "protagonist" {
			protagonists++
		}
		if status := strings.ToLower(field("status")); status != "candidate" && status != "canon" {
			vs = append(vs, Violation{Path: base + ".status", Want: "candidate（立项一律 candidate，由作者晋升）", Got: field("status")})
		}
		if md := field("cardMarkdown"); !strings.HasPrefix(strings.TrimSpace(md), "# ") {
			vs = append(vs, Violation{Path: base + ".cardMarkdown", Want: "第一行是 `# 纯姓名`", Got: "缺少标题行"})
		}
	}
	if protagonists == 0 {
		vs = append(vs, Violation{Path: "cast[].role", Want: "至少一个 protagonist", Got: "0 个"})
	}
	return contractFail("立项", vs)
}

// normalizeSalvagedFinding repairs a finding recovered from a truncated
// response. Salvage is best-effort recovery, so missing structure is filled
// rather than rejected — but never upward: a truncated output cannot be
// trusted to grade severity, so it is clamped to advisory.
func normalizeSalvagedFinding(f *domain.ReviewFinding, chapter, index int) {
	if f.Severity != severityBlocking && f.Severity != severityAdvisory {
		f.Severity = severityAdvisory
	}
	if strings.TrimSpace(f.ID) == "" {
		f.ID = fmt.Sprintf("salvaged-%d-%d", chapter, index+1)
	}
	if strings.TrimSpace(f.Title) == "" {
		f.Title = "审校输出不完整"
	}
	if strings.TrimSpace(f.Evidence) == "" {
		f.Evidence = "模型输出被截断，本条仅有部分内容"
	}
}

func sceneChapter(sc map[string]any) int {
	v, ok := sc["chapter"]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return 0
		}
		return int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

func rangeMissing(seen map[int]bool, from, to int) []int {
	var out []int
	for n := from; n <= to; n++ {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}

func rangeOutside(seen map[int]bool, from, to int) []int {
	var out []int
	for n := range seen {
		if n < from || n > to {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, "、")
}
