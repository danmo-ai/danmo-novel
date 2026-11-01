package chain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// seedProse 让单元先有正文，否则审校会以 "no chapters to review" 直接失败。
func seedProse(t *testing.T, h *harness) {
	t.Helper()
	ctx := context.Background()
	run, err := h.runs.Start(ctx, domain.TaskSpec{Kind: domain.TaskWrite, BookID: h.book, UnitID: "v01-U1"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := h.runs.WaitRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.CandidateID == "" {
		t.Fatalf("write produced no candidate: %s", done.Error)
	}
	snap, err := h.runs.Store.GetCandidate(ctx, done.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	_ = snap
	if _, err := h.runs.Accept(ctx, domain.AcceptCommand{CandidateID: done.CandidateID}); err != nil {
		t.Fatalf("accept write: %v", err)
	}
}

// 作者常常先点「审校」、再点「连续重写」。若那次审校的结果没人处理，候选会一直
// 占着这个单元，链必须明确告诉作者"先去处理它"，而不是抛一个 UUID 让他猜。
func TestSettleActiveRunReportsUnhandledCandidate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	seedProse(t, h)

	run, err := h.runs.Start(ctx, domain.TaskSpec{Kind: domain.TaskReview, BookID: h.book, UnitID: "v01-U1"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := h.runs.WaitRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != domain.RunWaiting {
		t.Fatalf("review status=%s err=%s", done.Status, done.Error)
	}

	start := time.Now()
	blocked, why := h.svc.settleActiveRun(ctx, &domain.Chain{BookID: h.book}, domain.ChainStageReview, "v01-U1")
	elapsed := time.Since(start)

	if !blocked {
		t.Fatal("an unhandled candidate must stop the chain")
	}
	for _, want := range []string{"v01-U1", "待处理", "审校"} {
		if !strings.Contains(why, want) {
			t.Fatalf("reason missing %q: %s", want, why)
		}
	}
	// 已经停在 waiting_accept 就不该再进等待分支（否则会挂满超时）。
	if elapsed > 3*time.Second {
		t.Fatalf("settleActiveRun waited %s on an already-settled run", elapsed)
	}
}

// 没有任何历史调用时不能误判为阻塞。
func TestSettleActiveRunCleanWhenNothingRunning(t *testing.T) {
	h := newHarness(t)
	blocked, why := h.svc.settleActiveRun(context.Background(), &domain.Chain{BookID: h.book}, domain.ChainStageOutline, "v01-U1")
	if blocked {
		t.Fatalf("should not block on a clean unit: %s", why)
	}
}

// 真让链跑完一遍，行为不能被这次改动破坏。
func TestChainStillCompletesNormally(t *testing.T) {
	h := newHarness(t)
	ch := h.start(t, domain.ChainSpec{})
	if ch.Status != domain.ChainCompleted {
		t.Fatalf("status=%s err=%s", ch.Status, ch.Error)
	}
}

// 中断原因必须保留：作者点几次「继续」后，第一次的根因不能被次生错误顶掉。
func TestInterruptKeepsPreviousReasons(t *testing.T) {
	h := newHarness(t)
	ch := &domain.Chain{ID: "c1", BookID: h.book, Status: domain.ChainRunning}

	_ = h.svc.interrupt(ch, domain.ChainStageReview, "", "门禁未通过：第 2 章连续性错误")
	_ = h.svc.interrupt(ch, domain.ChainStageReview, "", "单元 v01-U1 已有一份待处理的审校结果")

	if ch.Error != "单元 v01-U1 已有一份待处理的审校结果" {
		t.Fatalf("current error=%q", ch.Error)
	}
	if len(ch.PreviousErrors) == 0 || !strings.Contains(ch.PreviousErrors[0], "门禁未通过") {
		t.Fatalf("original reason lost: %+v", ch.PreviousErrors)
	}
	// 同一条重复中断不该无限堆叠。
	_ = h.svc.interrupt(ch, domain.ChainStageReview, "", "单元 v01-U1 已有一份待处理的审校结果")
	if len(ch.PreviousErrors) != 1 {
		t.Fatalf("duplicate reasons stacked: %+v", ch.PreviousErrors)
	}
}
