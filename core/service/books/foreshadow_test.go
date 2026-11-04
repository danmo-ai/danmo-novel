package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func foreshadowRegistry(entries string) string {
	return "# 伏笔登记表\n- schema_version: 1\n\n" + entries
}

func foreshadowSnap(entries string) domain.BookSnapshot {
	return domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b"},
		Files:   map[string]string{ForeshadowSchedulePath: foreshadowRegistry(entries)},
	}
}

func baseEntry(id, extra string) string {
	return "## 伏笔 · " + id + "\n" +
		"- content: 登记表的第 " + id + " 条\n" +
		"- type: 信息\n" +
		"- plant_volume: v01\n" +
		"- payoff_volume: v01\n" +
		"- payoff_summary: 读者得知答案\n" +
		"- status: planned\n" + extra
}

func TestValidateForeshadowScheduleAcceptsWellFormed(t *testing.T) {
	if err := ValidateForeshadowSchedule(foreshadowRegistry(baseEntry("FS-01", ""))); err != nil {
		t.Fatalf("合规条目被拦：%v", err)
	}
}

func TestValidateForeshadowScheduleRejectsMissingPayoffVolume(t *testing.T) {
	raw := foreshadowRegistry("## 伏笔 · FS-01\n- content: x\n- type: 物件\n- plant_volume: v01\n- status: planned\n")
	err := ValidateForeshadowSchedule(raw)
	if err == nil || !strings.Contains(err.Error(), "payoff_volume") {
		t.Fatalf("缺 payoff_volume 必须报出来：%v", err)
	}
}

func TestValidateForeshadowScheduleRejectsReversal(t *testing.T) {
	raw := foreshadowRegistry("## 伏笔 · FS-01\n- content: x\n- type: 物件\n- plant_volume: v03\n- payoff_volume: v01\n- status: planned\n")
	err := ValidateForeshadowSchedule(raw)
	if err == nil || !strings.Contains(err.Error(), "早于埋入卷") {
		t.Fatalf("回收早于埋入必须被拦：%v", err)
	}
}

// 同单元埋收是合法的三级细化中间态：总纲只知道「都在 v01-U1」。
func TestValidateForeshadowScheduleAllowsSameUnit(t *testing.T) {
	raw := foreshadowRegistry("## 伏笔 · FS-01\n- content: x\n- type: 物件\n" +
		"- plant_volume: v01\n- plant_unit: v01-U1\n- payoff_volume: v01\n- payoff_unit: v01-U1\n- status: planned\n")
	if err := ValidateForeshadowSchedule(raw); err != nil {
		t.Fatalf("同单元埋收不该被拦：%v", err)
	}
}

func TestValidateForeshadowScheduleRequiresSummaryForInformation(t *testing.T) {
	raw := foreshadowRegistry("## 伏笔 · FS-01\n- content: x\n- type: 信息\n- plant_volume: v01\n- payoff_volume: v01\n- status: planned\n")
	err := ValidateForeshadowSchedule(raw)
	if err == nil || !strings.Contains(err.Error(), "payoff_summary") {
		t.Fatalf("信息类缺回收摘要必须被拦：%v", err)
	}
	// type=物件 时不要求摘要。
	ok := foreshadowRegistry("## 伏笔 · FS-01\n- content: 那把钥匙\n- type: 物件\n- plant_volume: v01\n- payoff_volume: v01\n- status: planned\n")
	if err := ValidateForeshadowSchedule(ok); err != nil {
		t.Fatalf("物件类不该要求摘要：%v", err)
	}
}

func TestValidateForeshadowScheduleRejectsUnknownType(t *testing.T) {
	raw := foreshadowRegistry("## 伏笔 · FS-01\n- content: x\n- type: 情绪\n- plant_volume: v01\n- payoff_volume: v01\n- status: planned\n")
	err := ValidateForeshadowSchedule(raw)
	if err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("非法 type 必须被拦：%v", err)
	}
}

// 不做状态兼容：旧揭示计划的 locked 由迁移改写成 planned，运行期再出现就是错。
func TestValidateForeshadowScheduleRejectsLegacyLockedStatus(t *testing.T) {
	raw := foreshadowRegistry("## 伏笔 · FS-01\n- content: x\n- type: 信息\n- plant_volume: v01\n- payoff_volume: v01\n- payoff_summary: y\n- status: locked\n")
	err := ValidateForeshadowSchedule(raw)
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("locked 必须被当作非法状态：%v", err)
	}
}

func TestForeshadowUnitRefsDeriveFromPayoffPosition(t *testing.T) {
	entries := baseEntry("FS-01", "- plant_unit: v01-U1\n- payoff_unit: v01-U2\n") +
		baseEntry("FS-02", "- plant_unit: v01-U1\n- payoff_unit: v01-U5\n")
	snap := foreshadowSnap(entries)

	revealable, forbidden := ForeshadowUnitRefs(snap, "v01-U3")
	if !contains(revealable, "FS-01") {
		t.Fatalf("回收于 U2 的条目在 U3 应可揭示：%v", revealable)
	}
	if !contains(forbidden, "FS-02") {
		t.Fatalf("回收于 U5 的条目在 U3 应禁揭示：%v", forbidden)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestValidateForeshadowChapterRefsRejectsUnknownID(t *testing.T) {
	snap := foreshadowSnap(baseEntry("FS-01", ""))
	units := []domain.UnitOutlineDraft{{
		UnitID: "v01-U1",
		Chapters: []domain.ChapterPlan{{
			Chapter: 1, Plant: []string{"FS-99"},
		}},
	}}
	err := ValidateForeshadowChapterRefs(snap, units)
	if err == nil || !strings.Contains(err.Error(), "FS-99") {
		t.Fatalf("引用不存在的伏笔必须被拦：%v", err)
	}
}

func TestValidateForeshadowChapterRefsRejectsOtherUnitClaim(t *testing.T) {
	snap := foreshadowSnap(baseEntry("FS-01", "- plant_unit: v08-U1\n- payoff_unit: v08-U2\n"))
	units := []domain.UnitOutlineDraft{{
		UnitID: "v01-U1",
		Chapters: []domain.ChapterPlan{{
			Chapter: 1, Plant: []string{"FS-01"},
		}},
	}}
	err := ValidateForeshadowChapterRefs(snap, units)
	if err == nil || !strings.Contains(err.Error(), "v08-U1") {
		t.Fatalf("替别的单元埋收必须被拦：%v", err)
	}
}

func TestValidateForeshadowChapterRefsRejectsPayoffBeforePlant(t *testing.T) {
	snap := foreshadowSnap(baseEntry("FS-01", ""))
	units := []domain.UnitOutlineDraft{{
		UnitID: "v01-U1",
		Chapters: []domain.ChapterPlan{
			{Chapter: 5, Plant: []string{"FS-01"}},
			{Chapter: 2, Payoff: []string{"FS-01"}},
		},
	}}
	err := ValidateForeshadowChapterRefs(snap, units)
	if err == nil || !strings.Contains(err.Error(), "晚于埋入") {
		t.Fatalf("回收早于埋入必须被拦：%v", err)
	}
}

// 没有登记表的书（迁移前）一律放行，不能让旧书无法生产。
func TestValidateForeshadowChapterRefsNoopWithoutRegistry(t *testing.T) {
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b"}, Files: map[string]string{}}
	units := []domain.UnitOutlineDraft{{
		UnitID:   "v01-U1",
		Chapters: []domain.ChapterPlan{{Chapter: 1, Plant: []string{"FS-01"}}},
	}}
	if err := ValidateForeshadowChapterRefs(snap, units); err != nil {
		t.Fatalf("无登记表时不该拦：%v", err)
	}
}
