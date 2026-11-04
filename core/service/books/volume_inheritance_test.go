package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// volumeInheritanceFixture 构造一份合法的总纲 + 一卷卷纲，用于验证父子合同。
func volumeInheritanceFixture(goal string) (domain.BookSnapshot, string) {
	book := `# 全书总纲 · 测试
- schema_version: 1
- book_id: x
- title: 测试
- logline: 主角在困境中选择行动

## 故事线 · main
- kind: main
- description: 主线
- opening_state: 未开始
- ending_state: 已完成

## 分卷 · v01
- arc_id: arc-v01
- title: 第一卷
- chapter_range: 1-6
- purpose: 建立目标
- dramatic_question: 能否在质疑中完成目标？
- central_conflict: 目标与阻碍
- stakes: 失败代价
- entry_state: 未开始
- exit_state: 已完成
- thread_moves: main=未开始->已完成
- deliverables: 完成第一次行动
- open_questions: 下一步是什么
- plant_ids: FS-01
- payoff_ids: FS-02
- next_volume_entry: 继续行动

### 转折 · event-1
- event: 主角采取行动
- cause: 目标受到阻碍
- effect: 获得新的位置
- chapter_window: 1-6
`
	volume := `# 卷纲 · v01
- schema_version: 1
- volume_id: v01
- title: 第一卷
- source_arc_id: arc-v01
- chapter_range: 1-6
- goal: ` + goal + `
- dramatic_question: 能否在质疑中完成目标？
- thread_moves: main=未开始->已完成
- deliverables: 完成第一次行动
- central_conflict: 目标与阻碍
- stakes: 失败代价
- entry_state: 未开始
- exit_state: 已完成
- cast: lin

## 节拍 · event-1
- event: 主角申请复核
- cause: 旧材料存在疑点
- effect: 获得新的调查入口
- chapter_window: 1-6

## 单元 · v01-U1
- title: 起步
- chapter_range: 1-6
- objective: 获得行动资格
- obstacle: 尚未获得许可
- attempt: 查找旧材料
- setback: 材料不足
- choice: 按程序申请复核
- cost: 承担复核责任
- outcome: 获得三天复核许可
- entry_cause: v01-U1
- exit_result: 获得许可
- state_changes: lin.role=新人->复核人
- clock_start_day: 1
- clock_end_day: 6
- clock_gap_days: 0
- thread_ids: main
- event_ids: event-1
- plant_ids: FS-01
- payoff_ids: FS-02
- next_hook: 信息缺口 | 签名不一致
`
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "x", RootPath: "/tmp/none"},
		Files: map[string]string{
			"outline/book_outline.md":          book,
			"outline/volumes/v01.md":           volume,
			ForeshadowSchedulePath:             sampleSettings()[ForeshadowSchedulePath],
			"outline/volumes/v01.md#decorator": "ignore",
		},
		Cast: []domain.CastCard{{Stem: "lin", Name: "林晚", Status: domain.CastCanon}},
	}
	return snap, volume
}

// goal 是本卷的写作目标，dramatic_question 是要回答的悬念；两者不要求字面相同。
// 早期把它们绑成同一字段，会迫使模型把目标陈述改写成问句。
func TestVolumeGoalIsNotBoundToDramaticQuestion(t *testing.T) {
	snap, volume := volumeInheritanceFixture("用连续可见胜利验证搭档模式并完成基层立足。")
	if err := ValidateStructuredOutlineFile(snap, "outline/volumes/v01.md", volume); err != nil {
		t.Fatalf("goal 不应被强制等于 dramatic_question: %v", err)
	}
}

// 父级真正锁定的合同仍然逐字校验。
func TestVolumeStillInheritsEntryAndExitState(t *testing.T) {
	snap, volume := volumeInheritanceFixture("目标")
	broken := strings.Replace(volume, "- entry_state: 未开始", "- entry_state: 另一开局", 1)
	if err := ValidateStructuredOutlineFile(snap, "outline/volumes/v01.md", broken); err == nil || !strings.Contains(err.Error(), "entry_state") {
		t.Fatalf("entry_state 必须继承总纲: %v", err)
	}
	snap2, volume2 := volumeInheritanceFixture("目标")
	broken2 := strings.Replace(volume2, "- exit_state: 已完成", "- exit_state: 另一种收尾", 1)
	if err := ValidateStructuredOutlineFile(snap2, "outline/volumes/v01.md", broken2); err == nil || !strings.Contains(err.Error(), "exit_state") {
		t.Fatalf("exit_state 必须继承总纲: %v", err)
	}
}
