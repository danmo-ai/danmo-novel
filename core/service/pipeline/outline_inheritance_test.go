package pipeline

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func testSeed(fields map[string]string) domain.MarkdownEntity {
	e := domain.MarkdownEntity{Kind: "单元", ID: "v01-U1"}
	for k, v := range fields {
		e.Fields = append(e.Fields, domain.MarkdownField{Name: k, Value: v})
	}
	return e
}

func testSeedFields() map[string]string {
	return map[string]string{
		"title":           "起步",
		"chapter_range":   "1-3",
		"objective":       "取得首案侦办位置。",
		"obstacle":        "老警察不信见习生。",
		"attempt":         "用记忆指出复核方向。",
		"setback":         "一处细节已失真。",
		"choice":          "放弃把记忆当结论。",
		"cost":            "暴露异常敏锐。",
		"outcome":         "获得专案协助资格。",
		"clock_start_day": "1",
		"clock_end_day":   "6",
		"clock_gap_days":  "0",
		"next_hook":       "信息缺口 | 卷宗少了一页。",
		"state_changes":   "lu-chen.goal=自保->取得首案结果",
		"thread_ids":      "main",
		"entry_cause":     "重生后无信用。",
		"exit_result":     "成为可用办案人。",
	}
}

// inheritedUnitDraft 是一份完全合规的细纲：只继承、不改写。
func inheritedUnitDraft() domain.UnitOutlineDraft {
	return domain.UnitOutlineDraft{
		UnitID: "v01-U1", SourceSeedID: "v01-U1",
		Function: "取得首案侦办位置。", Desire: "取得首案侦办位置。",
		Obstacle: "老警察不信见习生。", Choice: "放弃把记忆当结论。",
		Payoff: "获得专案协助资格。", Attempt: "用记忆指出复核方向。",
		Setback: "一处细节已失真。", Cost: "暴露异常敏锐。",
		ChapterFrom: 1, ChapterTo: 3,
		StoryDay: "1", EndDay: 6, GapDays: 0,
		StateDeltas: []any{map[string]any{"stem": "lu-chen", "field": "goal", "from": "自保", "to": "取得首案结果"}},
		NextHook:    map[string]any{"type": "信息缺口", "out": "卷宗少了一页。"},
	}
}

func TestUnitSeedInheritanceAcceptsFaithfulDraft(t *testing.T) {
	if err := ValidateUnitSeedInheritance(inheritedUnitDraft(), testSeed(testSeedFields())); err != nil {
		t.Fatalf("合规细纲不该被拦：%v", err)
	}
}

// function 与 desire 同源于种子的 objective；以前只校验 desire，
// 模型改写 function 时无人发现，单元在卷里的功能定位会静默漂移。
func TestUnitSeedInheritanceLocksFunction(t *testing.T) {
	draft := inheritedUnitDraft()
	draft.Function = "报到当天首案重启：取得首案侦办位置。" // 换一种说法也不行
	err := ValidateUnitSeedInheritance(draft, testSeed(testSeedFields()))
	if err == nil || !strings.Contains(err.Error(), "function") {
		t.Fatalf("function 改写必须被拦：%v", err)
	}
}

// next_hook 的类型由卷纲定，细纲只写具体事件。
func TestUnitSeedInheritanceLocksStateTransitions(t *testing.T) {
	draft := inheritedUnitDraft()
	draft.StateDeltas = []any{map[string]any{"stem": "lu-chen", "field": "goal", "from": "自保", "to": "换成另一个故事"}}
	if err := ValidateUnitSeedInheritance(draft, testSeed(testSeedFields())); err == nil || !strings.Contains(err.Error(), "stateDeltas") {
		t.Fatalf("state transition must inherit the volume seed: %v", err)
	}
}

func TestUnitSeedInheritanceLocksNextHookType(t *testing.T) {
	draft := inheritedUnitDraft()
	draft.NextHook = map[string]any{"type": "高代价选择", "out": "卷宗少了一页。"}
	err := ValidateUnitSeedInheritance(draft, testSeed(testSeedFields()))
	if err == nil || !strings.Contains(err.Error(), "nextHook.type") {
		t.Fatalf("钩子类型改写必须被拦：%v", err)
	}
	// 报错文本不能出现 <nil>，作者要看得懂缺的是什么。
	draft.NextHook = nil
	err = ValidateUnitSeedInheritance(draft, testSeed(testSeedFields()))
	if err == nil || strings.Contains(err.Error(), "<nil>") {
		t.Fatalf("缺失 nextHook 的报错应可读：%v", err)
	}
}

// 种子没写类型时（只有事件）不做强制，避免旧卷纲被一刀切拦下。
func TestUnitSeedInheritanceSkipsEmptyHookType(t *testing.T) {
	fields := testSeedFields()
	fields["next_hook"] = "卷宗少了一页。"
	draft := inheritedUnitDraft()
	draft.NextHook = nil
	if err := ValidateUnitSeedInheritance(draft, testSeed(fields)); err != nil {
		t.Fatalf("种子无类型时不该强制：%v", err)
	}
}
