package gates

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 内置规则文件必须能解析：正则写错时宁可测试红，也不要静默失去检查。
func TestBuiltinRulesLoad(t *testing.T) {
	rs := BuiltinRules()
	if rs == nil || len(rs.Signals) < 10 {
		t.Fatalf("signals=%d, want the shipped rule set", len(rs.Signals))
	}
	if rs.ClusterAt < 2 {
		t.Fatalf("clusterAt=%d", rs.ClusterAt)
	}
	for _, sig := range rs.Signals {
		if sig.re == nil {
			t.Fatalf("signal %s has no compiled pattern", sig.ID)
		}
	}
}

// 作者覆盖必须优先：本书 > 作者机器 > 内置，且三层的来源标签可区分。
func TestResolveRulesPrecedence(t *testing.T) {
	book := `
signals:
  - id: onlybook
    title: 本书规则
    severity: advisory
    pattern: '本书专用词'
`
	authorRules := `
signals:
  - id: onlyauthor
    title: 作者机器规则
    severity: advisory
    pattern: '作者机器专用词'
`
	rs, err := ResolveRules(book, authorRules)
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != RulesFromBook || rs.Signals[0].ID != "onlybook" {
		t.Fatalf("book should win: source=%s id=%s", rs.Source, rs.Signals[0].ID)
	}

	rs, err = ResolveRules("", authorRules)
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != RulesFromAuthor || rs.Signals[0].ID != "onlyauthor" {
		t.Fatalf("author override should win: source=%s", rs.Source)
	}

	rs, err = ResolveRules("", "")
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != RulesFromBuiltin {
		t.Fatalf("fallback source=%s", rs.Source)
	}
}

// 写错的正则是硬错误：静默退化会让作者以为检查在跑。
func TestResolveRulesRejectsBadPattern(t *testing.T) {
	_, err := ResolveRules("signals:\n  - id: x\n    title: t\n    severity: advisory\n    pattern: '('\n", "")
	if err == nil {
		t.Fatal("invalid regex must fail loudly")
	}
	if !strings.Contains(err.Error(), "x") {
		t.Fatalf("error should name the signal: %v", err)
	}
}

func TestResolveRulesRejectsUnknownSeverity(t *testing.T) {
	_, err := ResolveRules("signals:\n  - id: x\n    title: t\n    severity: fatal\n    pattern: 'a'\n", "")
	if err == nil {
		t.Fatal("unknown severity must fail")
	}
}

// 密度判定：短章不会被小样本放大，长章不会漏报。
func TestSignalUsesDensityNotRawCount(t *testing.T) {
	rs := BuiltinRules()
	// 200 字里 3 个破折号：密度 15/千字，但绝对数远低于 minHits=9 → 不报。
	short := domain.ChapterDraft{Chapter: 1, Body: strings.Repeat("字", 194) + "————"}
	if got := findTitle(CheckProseWithRules([]domain.ChapterDraft{short}, 0, rs), "破折号过多"); got {
		t.Fatal("short chapter with few dashes must not trip the block")
	}
	// 2000 字里 12 个破折号：密度 6/千字 且过 minHits → 报。
	long := domain.ChapterDraft{Chapter: 1, Body: strings.Repeat("字", 1976) + strings.Repeat("——", 12)}
	if !findTitle(CheckProseWithRules([]domain.ChapterDraft{long}, 0, rs), "破折号过多") {
		t.Fatal("long chapter with dense dashes should trip")
	}
}

// 翻案腔是最强信号：一处即报，且级别为 blocking。
func TestFlipConstructionBlocks(t *testing.T) {
	body := strings.Repeat("他站起来走了两步。", 180) + "这不是运气，而是有人提前安排好的。"
	rep := CheckProseWithRules([]domain.ChapterDraft{{Chapter: 3, Body: body}}, 0, nil)
	var hit *domain.ReviewFinding
	for i := range rep.Findings {
		if rep.Findings[i].Title == "翻案腔（不是 A 而是 B）" {
			hit = &rep.Findings[i]
		}
	}
	if hit == nil {
		t.Fatal("flip construction not detected")
	}
	if hit.Severity != "blocking" || hit.Chapter != 3 {
		t.Fatalf("finding=%+v", hit)
	}
	if rep.OK {
		t.Fatal("blocking finding must fail the report")
	}
}

// 单个弱信号不定罪：只有多类同时命中才补「成簇」。
func TestWeakSignalsOnlyCluster(t *testing.T) {
	rs := BuiltinRules()
	body := strings.Repeat("他记下时间，翻过一页。", 200) + "核心是：他早就知道。"
	rep := CheckProseWithRules([]domain.ChapterDraft{{Chapter: 1, Body: body}}, 0, rs)
	for _, f := range rep.Findings {
		if f.Title == "结论提示冒号" {
			t.Fatal("weak signals must never surface on their own")
		}
	}
	if !rep.OK {
		t.Fatalf("single weak signal must not fail: %+v", rep.Findings)
	}
	// 多类同时命中 → 成簇提示（advisory，不阻断）。
	noisy := strings.Repeat("他记下时间，翻过一页。", 200) +
		"核心是：他早就知道。以下几种情况：✦✦ 他非常非常非常非常害怕，手指发抖。"
	rep = CheckProseWithRules([]domain.ChapterDraft{{Chapter: 1, Body: noisy}}, 0, rs)
	if !findTitle(rep, "成簇的 AI 文风特征") {
		t.Fatalf("cluster hint missing: %+v", titles(rep))
	}
}

func findTitle(rep Report, title string) bool {
	for _, f := range rep.Findings {
		if f.Title == title {
			return true
		}
	}
	return false
}

func titles(rep Report) []string {
	out := make([]string, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		out = append(out, f.Title)
	}
	return out
}

// 细纲里的字数额度必须落在单章区间内：30000/6 章 = 5000 字/章，
// 与 2000–3500 打架，应该在细纲阶段就拦下，而不是等正文写完才发现。
func TestCheckUnitOutlineRejectsOutOfRangeWordTarget(t *testing.T) {
	base := domain.UnitOutlineDraft{
		UnitID: "v01-U1", Function: "功能", Desire: "欲望", Obstacle: "阻碍", Choice: "选择",
		POV: "lin", OnStage: []string{"lin"}, ChapterFrom: 1, ChapterTo: 6,
	}
	bad := base
	bad.WordTarget = "30000"
	rep := CheckUnitOutline(bad, nil)
	if rep.OK {
		t.Fatal("30000 over 6 chapters is 5000/chapter and must be rejected")
	}
	var hit bool
	for _, f := range rep.Findings {
		if f.ID == "outline-word-target" {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("missing word_target finding: %+v", rep.Findings)
	}

	// 15000/6 = 2500，合法。
	good := base
	good.WordTarget = "15000"
	if rep := CheckUnitOutline(good, nil); !rep.OK {
		t.Fatalf("15000 over 6 chapters should pass: %+v", rep.Findings)
	}

	// chapter_chars 显式越界同样拦。
	cc := base
	cc.ChapterChars = "5000"
	if rep := CheckUnitOutline(cc, nil); rep.OK {
		t.Fatal("chapter_chars=5000 must be rejected")
	}

	// 老书回写带取整误差（21274/10 章 ≈ 2127.4）不该被拦。
	old := base
	old.ChapterTo = 10
	old.WordTarget = "21274"
	if rep := CheckUnitOutline(old, nil); !rep.OK {
		t.Fatalf("rounded legacy word_target should pass: %+v", rep.Findings)
	}
}

// 超长只报 advisory：审校页没法对整章做大幅度缩写，做成 blocking 只会
// 把作者卡在修复一步（重写一遍大概率还是那么长）。真正的约束在写作提示词里。
func TestOverLengthIsAdvisoryNotBlocking(t *testing.T) {
	body := strings.Repeat("字", 4000) // 2500 × 1.4 = 3500 以上
	rep := CheckProseWithRules([]domain.ChapterDraft{{Chapter: 1, Body: body}}, 2500, nil)
	if !rep.OK {
		t.Fatalf("over-length must not block: %+v", rep.Findings)
	}
	var found bool
	for _, f := range rep.Findings {
		if f.Title == "字数超出目标区间" {
			found = true
			if f.Severity != "advisory" {
				t.Fatalf("severity=%q want advisory", f.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("over-length must still be reported: %+v", rep.Findings)
	}
	// 字数不足仍然是 blocking：那是能靠补写修掉的问题。
	short := CheckProseWithRules([]domain.ChapterDraft{{Chapter: 1, Body: strings.Repeat("字", 800)}}, 2500, nil)
	if short.OK {
		t.Fatal("short chapter must still block")
	}
}
