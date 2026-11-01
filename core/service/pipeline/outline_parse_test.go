package pipeline

import (
	"strings"
	"testing"
)

func TestParseOutlineUnitsEnvelopeAndBare(t *testing.T) {
	units, err := parseOutlineUnits(`{"units":[{"unitId":"v01-U1","title":"新警"}]}`)
	if err != nil || len(units) != 1 || units[0].UnitID != "v01-U1" {
		t.Fatalf("envelope: %+v err=%v", units, err)
	}
	units, err = parseOutlineUnits(`{"unitId":"v01-U1","title":"黄金三章","desire":"立足"}`)
	if err != nil || len(units) != 1 || units[0].UnitID != "v01-U1" {
		t.Fatalf("bare unit: %+v err=%v", units, err)
	}
	if _, err := parseOutlineUnits(`{"units":[]}`); err == nil {
		t.Fatal("empty units should fail")
	}
}

func TestParseOutlineUnitsCoercesStoryDayObject(t *testing.T) {
	raw := `{"units":[{"unitId":"v01-U1","title":"新警","storyDay":{"day":3,"label":"入职第三天"},"chapterFrom":"1","chapterTo":3}]}`
	units, err := parseOutlineUnits(raw)
	if err != nil {
		t.Fatal(err)
	}
	if units[0].StoryDay != "3 入职第三天" {
		t.Fatalf("storyDay=%q", units[0].StoryDay)
	}
	if units[0].ChapterFrom != 1 || units[0].ChapterTo != 3 {
		t.Fatalf("chapters=%d-%d", units[0].ChapterFrom, units[0].ChapterTo)
	}
}

func TestParseOutlineUnitsCoercesStringNextHook(t *testing.T) {
	raw := `{"units":[{"unitId":"v08-U4","title":"送上去","nextHook":"台历停在七月那一格，笔停在纸上。","chapterFrom":381,"chapterTo":390}]}`
	units, err := parseOutlineUnits(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := units[0].NextHook["out"]; got != "台历停在七月那一格，笔停在纸上。" {
		t.Fatalf("nextHook.out=%v", got)
	}
	// 对象形态保持不变
	raw2 := `{"units":[{"unitId":"v08-U4","nextHook":{"type":"未完成动作","out":"笔停在纸上。"}}]}`
	units2, err := parseOutlineUnits(raw2)
	if err != nil {
		t.Fatal(err)
	}
	if units2[0].NextHook["type"] != "未完成动作" || units2[0].NextHook["out"] != "笔停在纸上。" {
		t.Fatalf("object nextHook=%v", units2[0].NextHook)
	}
}

func TestSalvageReviewJSON(t *testing.T) {
	// Truncated mid-findings
	raw := `{"verdict":"revise","findings":[{"id":"f1","severity":"blocking","source":"ai_review","lens":"continuity","title":"时钟冲突","evidence":"story_day 倒退"},{"id":"f2","severity":"advisory","lens":"style","title":"词频","evidence":"口头禅重复`
	v, f, truncated, err := salvageReviewJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v != "revise" || len(f) == 0 {
		t.Fatalf("verdict=%q findings=%d", v, len(f))
	}
	if !truncated {
		t.Fatal("truncated mid-string must be reported so the caller can warn")
	}
	// JSON followed by chatter
	raw2 := `{"verdict":"pass","findings":[]}
以上是我的分析。`
	v2, f2, trunc2, err2 := salvageReviewJSON(raw2)
	if err2 != nil || v2 != "pass" || f2 == nil {
		t.Fatalf("err=%v v=%q", err2, v2)
	}
	if trunc2 {
		t.Fatal("complete JSON with chatter is not truncation")
	}
}

// 真实故障：输出在某条 finding 的字符串中间被预算切断，全文再无闭合 "}"。
// 半条结论不算结论：这里必须报错，让 runJSON 用更大预算重试，
// 而不是把残缺对象补成一条"看起来成立"的问题去污染审校清单。
func TestSalvageReviewJSONTruncatedInsideFirstFinding(t *testing.T) {
	raw := `{"verdict":"revise","findings":[{"id":"ai-1","severity":"blocking","source":"ai_review","lens":"continuity","title":"位置跳变","evidence":"第 7 章他还在城东，第 8 章没有交代就到了码头，前文没有车程也没有时间推移，这一处连续性断了，读者会`
	v, f, truncated, err := salvageReviewJSON(raw)
	if err == nil {
		t.Fatalf("half a finding must not be accepted: v=%q f=%+v", v, f)
	}
	if !truncated {
		t.Fatal("must flag truncation so the caller can escalate the budget")
	}
	if len(f) != 0 {
		t.Fatalf("findings=%+v, want none", f)
	}
}

// 前面几条已经写完、只有最后一条被切断时，完整的那几条仍然有效。
func TestSalvageReviewJSONKeepsCompleteFindings(t *testing.T) {
	raw := `{"verdict":"revise","findings":[{"id":"ai-1","severity":"blocking","title":"时钟冲突","evidence":"第3章是夜里，第4章又变清晨"},{"id":"ai-2","severity":"advisory","title":"口头禅","eviden`
	v, f, truncated, err := salvageReviewJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v != "revise" || !truncated {
		t.Fatalf("v=%q truncated=%v", v, truncated)
	}
	if len(f) != 1 || f[0].Title != "时钟冲突" {
		t.Fatalf("findings=%+v", f)
	}
}

// 旧的失败现场：模型先写一段分析再给 JSON，整段对象其实是闭合的。
func TestSalvageReviewJSONAfterPreamble(t *testing.T) {
	raw := "我先通读一遍正文。\n\n" + `{"verdict":"pass","findings":[]}`
	v, f, truncated, err := salvageReviewJSON(raw)
	if err != nil || v != "pass" || truncated || f == nil {
		t.Fatalf("err=%v v=%q truncated=%v", err, v, truncated)
	}
	if !strings.Contains(raw, "通读") {
		t.Fatal("sanity")
	}
}
