package pipeline

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func validChapterUnit() domain.UnitOutlineDraft {
	return domain.UnitOutlineDraft{
		UnitID: "v01-U1", Title: "起步", Function: "建立目标", Desire: "获得许可", Obstacle: "程序限制",
		Choice: "按程序复查", Payoff: "获得三天许可", Forbidden: "不提前揭示", InfoControl: "只知疑点",
		ChapterFrom: 1, ChapterTo: 2, StoryDay: "3", EndDay: 4, GapDays: 1,
		StateDeltas: []map[string]any{{"stem": "lin", "field": "goal", "from": "申请", "to": "复查"}},
		NextHook:    map[string]any{"type": "信息缺口", "out": "签名不一致"},
		Chapters: []domain.ChapterPlan{
			{Chapter: 1, Goal: "取得程序入口", Change: "获得复查许可", CutHook: "签名不合", DayOffset: 0},
			{Chapter: 2, Goal: "核对现场", Change: "固定第一条实证", CutHook: "证据指向旧案", DayOffset: 1},
		},
	}
}

func TestChapterPlanDoesNotRequireScenes(t *testing.T) {
	unit := validChapterUnit()
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err != nil {
		t.Fatalf("scene-less chapter plans must be writable: %v", err)
	}
}

func TestChapterPlanRejectsCoverageAndClockErrors(t *testing.T) {
	unit := validChapterUnit()
	unit.Chapters[1].DayOffset = 4
	err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID})
	if err == nil || !strings.Contains(err.Error(), "结束日") {
		t.Fatalf("out-of-range day offset must be rejected: %v", err)
	}
	unit = validChapterUnit()
	unit.Chapters = unit.Chapters[:1]
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err == nil || !strings.Contains(err.Error(), "缺第 2 章") {
		t.Fatalf("missing chapter must be rejected: %v", err)
	}
	unit = validChapterUnit()
	unit.StoryDay = "第3–4日"
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err == nil {
		t.Fatal("free-form story day must not enter production")
	}
}

func TestChapterPlanAllowsMinimalOptionalScene(t *testing.T) {
	unit := validChapterUnit()
	unit.Chapters[0].Scenes = []domain.ScenePlan{{Chapter: 1, Goal: "争取复查", Conflict: "疑点不足", Turn: "申请重勘", Outcome: "获得许可", MustLand: []string{"明确三天期限"}}}
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err != nil {
		t.Fatalf("minimal scene should pass: %v", err)
	}
	unit.Chapters[0].Scenes[0].MustLand = []string{"一", "二", "三"}
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err == nil {
		t.Fatal("scene must not become another prose-level checklist")
	}
}
