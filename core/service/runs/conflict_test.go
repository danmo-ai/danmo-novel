package runs

import (
	"strings"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 冲突提示必须说人话：旧实现只回 `active run already exists: <uuid>`，
// 作者既不知道是谁占着单元，也不知道该等还是该去点哪里。
//
// 审校清单是特例：界面上它没有"接受"按钮，只有通过/强制通过/拒绝清单，
// 所以提示绝不能叫作者"去接受"——那是一个不存在的按钮。
func TestActiveRunConflictExplainsWaitingAccept(t *testing.T) {
	active := domain.Run{
		ID: "ca25903a-ef75-4153-b96e-bbdcde4873fd", Kind: domain.TaskReview,
		Status: domain.RunWaiting, CandidateID: "9ecdf100-c975-41b5-bb23-378ba552f738",
		CreatedAt: time.Now().Add(-3 * time.Minute),
	}
	err := activeRunConflict(active, domain.TaskSpec{Kind: domain.TaskReview, UnitID: "v01-U1"})
	msg := err.Error()

	for _, want := range []string{
		"v01-U1",   // 哪个单元
		"审校清单",     // 哪种调用（人话，不是枚举值）
		"自动修复",     // 作者该做什么：界面上真实存在的动作
		"拒绝",       //
		"重新审校",     // 另一条出路：重新审校会先归档旧清单
		"9ecdf100", // 哪个候选（短 id，够定位到 UI）
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	// 不再把裸 UUID 当唯一信息，也不再指向不存在的按钮。
	if strings.Contains(msg, "active run already exists") {
		t.Fatalf("raw english error leaked: %s", msg)
	}
	if strings.Contains(msg, "接受或拒绝") {
		t.Fatalf("review lists have no 接受 button; hint must not send the author there: %s", msg)
	}
}

func TestActiveRunConflictExplainsInFlight(t *testing.T) {
	active := domain.Run{
		ID: "b4de2039-a064-4c9e-8906-bbc1f6ad78f2", Kind: domain.TaskWrite,
		Status: domain.RunRunning, CreatedAt: time.Now().Add(-90 * time.Second),
	}
	msg := activeRunConflict(active, domain.TaskSpec{Kind: domain.TaskWrite, UnitID: "v02-U3"}).Error()
	for _, want := range []string{"v02-U3", "正文", "运行中", "等它结束", "1 分钟"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

// 全书级任务（没有 unitId）不能显示成"单元 "。
func TestActiveRunConflictHandlesWholeBookTarget(t *testing.T) {
	active := domain.Run{Kind: domain.TaskPlan, Status: domain.RunRunning, CreatedAt: time.Now()}
	msg := activeRunConflict(active, domain.TaskSpec{Kind: domain.TaskPlan}).Error()
	if !strings.Contains(msg, "全书") {
		t.Fatalf("message should say 全书, got: %s", msg)
	}
	if strings.Contains(msg, "单元  ") || strings.Contains(msg, "单元 的") {
		t.Fatalf("dangling unit placeholder: %s", msg)
	}
}

// 提示里的类型必须指认「谁占着单元」，不能照抄作者想发起的任务。
// 旧实现把待接受的审校清单说成"待处理的修订结果"，作者于是去找一条
// 根本不存在的修订候选，越找越乱。
func TestActiveRunConflictNamesTheBlockingKind(t *testing.T) {
	active := domain.Run{
		Kind: domain.TaskWrite, Status: domain.RunWaiting,
		CandidateID: "12345678-0000-0000-0000-000000000000",
	}
	msg := activeRunConflict(active, domain.TaskSpec{Kind: domain.TaskReview, UnitID: "v01-U2"}).Error()
	for _, want := range []string{"待处理的正文结果", "再发起审校", "12345678"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "待处理的审校结果") {
		t.Fatalf("message must name the blocking run, not the requested one:\n%s", msg)
	}
}
