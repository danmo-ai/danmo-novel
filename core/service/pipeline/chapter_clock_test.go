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
			{Chapter: 1, Goal: "取得程序入口", Change: "获得复查许可", CutHook: "签名不合", DayOffset: 0,
				Scenes: []domain.ScenePlan{{Chapter: 1, Goal: "用疑点争取复查", Conflict: "疑点不足以翻案", Turn: "申请现场重勘", Outcome: "获得三天许可"}}},
			{Chapter: 2, Goal: "核对现场", Change: "固定第一条实证", CutHook: "证据指向旧案", DayOffset: 1,
				Scenes: []domain.ScenePlan{{Chapter: 2, Goal: "重勘现场", Conflict: "痕迹被清理", Turn: "改查签名笔迹", Outcome: "固定签名异常"}}},
		},
	}
}

func TestChapterPlanRequiresScenesEveryChapter(t *testing.T) {
	unit := validChapterUnit()
	unit.Chapters[1].Scenes = nil
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err == nil || !strings.Contains(err.Error(), "缺第 2 章的场面") {
		t.Fatalf("chapter without scenes must be rejected: %v", err)
	}
	unit = validChapterUnit()
	unit.Chapters[0].Scenes = append(unit.Chapters[0].Scenes,
		domain.ScenePlan{Chapter: 1, Goal: "b", Conflict: "c", Turn: "d", Outcome: "e"},
		domain.ScenePlan{Chapter: 1, Goal: "f", Conflict: "g", Turn: "h", Outcome: "i"},
		domain.ScenePlan{Chapter: 1, Goal: "j", Conflict: "k", Turn: "l", Outcome: "m"},
	)
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err == nil || !strings.Contains(err.Error(), "最多 3 个场面") {
		t.Fatalf("more than 3 scenes per chapter must be rejected: %v", err)
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

func TestChapterPlanValidatesSceneFields(t *testing.T) {
	unit := validChapterUnit()
	unit.Chapters[0].Scenes[0].MustLand = []string{"一", "二", "三"}
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err == nil {
		t.Fatal("scene must not become another prose-level checklist")
	}
	unit = validChapterUnit()
	unit.Chapters[0].Scenes[0].Turn = ""
	if err := ValidateOutlineContract([]domain.UnitOutlineDraft{unit}, []string{unit.UnitID}); err == nil || !strings.Contains(err.Error(), "goal/conflict/turn/outcome") {
		t.Fatalf("scene missing turn must be rejected: %v", err)
	}
}
