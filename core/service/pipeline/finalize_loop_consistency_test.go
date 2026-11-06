package pipeline

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 账本自洽必须在**生成阶段**就拦，而不是等到 Accept。
//
// 真实事故（mindvoice-cop v03-U5 ch134）：模型输出
//
//	{"chapter":134, "plant":["FS-12"], "plannedForeshadowHandled": null}
//
// 整单元核对调用"成功"（1 次调用、无 error），候选落库；作者点接受时才被
// books.StateFromActual 拒绝——那时已经没有修复机会，只能整份重跑，而这份
// 候选花了约 1.3 万 output token。
//
// 定稿包本来就有「一次定向修复」的预算（MaxCalls=2），只要生成阶段也查这条，
// 模型就能在同一轮里把漏掉的条目补上。
func TestValidateChapterActualRejectsSelfInconsistentLoops(t *testing.T) {
	a := domain.ChapterActual{
		Chapter:     134,
		Summary:     "复盘确认异常案另立一案。",
		Plant:       []string{"FS-12"},
		StateDeltas: []domain.ChapterActualDelta{},
	}
	_, err := validateChapterActual(134, a, "正文")
	if err == nil {
		t.Fatal("账本声明了 plant 却漏进 plannedForeshadowHandled，必须被拒")
	}
	if !strings.Contains(err.Error(), "FS-12") {
		t.Fatalf("错误必须点名缺哪一条，实际: %v", err)
	}
}

func TestValidateChapterActualAcceptsConsistentLoops(t *testing.T) {
	a := domain.ChapterActual{
		Chapter:                  134,
		Summary:                  "复盘确认异常案另立一案。",
		Plant:                    []string{"FS-12"},
		Payoff:                   []string{"FS-03"},
		PlannedForeshadowHandled: []string{"FS-12", "FS-03"},
		StateDeltas:              []domain.ChapterActualDelta{},
	}
	if _, err := validateChapterActual(134, a, "正文"); err != nil {
		t.Fatalf("自洽的账本应通过: %v", err)
	}
}

// 没有声明任何 plant/payoff 的章不受影响（大多数章并不埋收伏笔）。
func TestValidateChapterActualAllowsEmptyLoops(t *testing.T) {
	a := domain.ChapterActual{Chapter: 5, Summary: "一章", StateDeltas: []domain.ChapterActualDelta{}}
	if _, err := validateChapterActual(5, a, "正文"); err != nil {
		t.Fatalf("无伏笔的章应通过: %v", err)
	}
}
