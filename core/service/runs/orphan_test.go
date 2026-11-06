package runs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// 孤儿 run 必须能被回收，否则它会永久占住单元。
//
// 现场表现：某次 run 的 goroutine 中途消失，库里一条 status=running 的记录一直
// 留着。blockingRun 按「同单元有活跃 run」判，于是该单元的写正文/审校全部被
// 「已有一份待处理」拒绝——只有重启进程才解得开。同一单元被卡了 95 分钟。
func TestReapOrphansMarksDeadRunsFailed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db, settings, llm.NewMock())
	ctx := context.Background()

	// 一条「活着」的：s.cancels 里有它。
	live := domain.Run{
		ID: "run-live", BookID: "b1", Kind: domain.TaskWrite, Status: domain.RunRunning,
		CreatedAt: time.Now().Add(-10 * time.Minute), UpdatedAt: time.Now(),
	}
	// 一条「孤儿」：同样 running，但进程内没有任何执行体。
	orphan := domain.Run{
		ID: "run-orphan", BookID: "b1", Kind: domain.TaskWrite, Status: domain.RunRunning,
		CreatedAt: time.Now().Add(-10 * time.Minute), UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	for _, r := range []domain.Run{live, orphan} {
		if err := db.SaveRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	svc.mu.Lock()
	svc.cancels["run-live"] = func() {}
	svc.mu.Unlock()

	n, err := svc.ReapOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("应回收 1 条，实际 %d", n)
	}
	got, err := db.GetRun(ctx, "run-orphan")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.RunFailed {
		t.Fatalf("孤儿 run 应被标为 failed，实际 %s", got.Status)
	}
	gotLive, err := db.GetRun(ctx, "run-live")
	if err != nil {
		t.Fatal(err)
	}
	if gotLive.Status != domain.RunRunning {
		t.Fatalf("有执行体的 run 不能被动，实际 %s", gotLive.Status)
	}
}

// 刚创建的 run 有 30 秒宽限：startLocked 登记 cancels 与巡检读表可能同拍，
// 抢在这里回收会把一个正常起步的任务判死。
func TestReapOrphansSkipsJustCreated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	settings, _ := config.Load()
	db, err := sqlite.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db, settings, llm.NewMock())
	ctx := context.Background()

	fresh := domain.Run{
		ID: "run-fresh", BookID: "b1", Kind: domain.TaskOutline, Status: domain.RunRunning,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.SaveRun(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	n, err := svc.ReapOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("刚创建的 run 不应被回收，实际回收 %d", n)
	}
}

// 心跳必须真的把 callsUsed 写进库里——这是区分「长任务」与「死任务」的唯一信号。
func TestTouchRunPersistsHeartbeat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	settings, _ := config.Load()
	db, err := sqlite.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db, settings, llm.NewMock())
	ctx := context.Background()

	run := domain.Run{
		ID: "run-hb", BookID: "b1", Kind: domain.TaskWrite, Status: domain.RunRunning,
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
	}
	if err := db.SaveRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	svc.touchRun(ctx, run, 7)
	got, err := db.GetRun(ctx, "run-hb")
	if err != nil {
		t.Fatal(err)
	}
	if got.CallsUsed != 7 {
		t.Fatalf("心跳应写入 callsUsed=7，实际 %d", got.CallsUsed)
	}
	if time.Since(got.UpdatedAt) > time.Minute {
		t.Fatalf("心跳应刷新 updatedAt，实际 %s", got.UpdatedAt)
	}

	// 已终结的 run 不能被心跳改回 running。
	got.Status = domain.RunCancelled
	if err := db.SaveRun(ctx, got); err != nil {
		t.Fatal(err)
	}
	svc.touchRun(ctx, run, 99)
	after, err := db.GetRun(ctx, "run-hb")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.RunCancelled || after.CallsUsed != 7 {
		t.Fatalf("已取消的 run 不应被心跳复活：status=%s calls=%d", after.Status, after.CallsUsed)
	}
}
