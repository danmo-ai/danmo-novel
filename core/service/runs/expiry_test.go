package runs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// 回归：候选因书籍改动而过期时，它的 run 必须一起收尾。
//
// 以前只把候选标成 expired：候选从「待审候选」列表里消失，run 却永远停在
// waiting_accept，于是单元被永久占住——作者看不到那条候选，也就没有任何按钮
// 能解开，再点生成只收到「已有一份待处理的细纲结果」。
func TestExpireCandidateClosesItsRun(t *testing.T) {
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
	ctx := context.Background()

	root := filepath.Join(home, "book")
	src, _ := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan"))
	copyBookDir(t, src, root)
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	unitID := snap.Units[0].UnitID
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}

	run := domain.Run{
		ID: "run-outline", BookID: snap.Summary.ID, Kind: domain.TaskOutline, Status: domain.RunWaiting,
		Spec:      domain.TaskSpec{Kind: domain.TaskOutline, BookID: snap.Summary.ID, UnitID: unitID},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.SaveRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	// 候选绑在一个已经不存在于书里的 revision 上。
	cand := domain.Candidate{
		ID: "cand-outline", RunID: run.ID, BookID: snap.Summary.ID, Kind: domain.TaskOutline,
		Status: domain.CandidateOpen, BaseRevision: "stale-revision", UnitID: unitID,
	}
	if err := db.SaveCandidate(ctx, cand); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetRun(ctx, run.ID); err != nil || got.Status != domain.RunWaiting {
		t.Fatalf("run did not start in waiting_accept: %+v %v", got, err)
	}
	run.CandidateID = cand.ID
	if err := db.SaveRun(ctx, run); err != nil {
		t.Fatal(err)
	}

	svc := New(db, settings, llm.NewMock())
	if _, err := svc.Accept(ctx, domain.AcceptCommand{CandidateID: cand.ID}); err == nil {
		t.Fatal("stale candidate must not be accepted")
	}

	if got, _ := db.GetCandidate(ctx, cand.ID); got.Status != domain.CandidateExpired {
		t.Fatalf("candidate status=%s want expired", got.Status)
	}
	got, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == domain.RunWaiting {
		t.Fatal("expired candidate left its run in waiting_accept; the unit stays blocked forever")
	}
	if got.Status != domain.RunExpired {
		t.Fatalf("run status=%s want expired", got.Status)
	}
	if strings.TrimSpace(got.Error) == "" {
		t.Fatal("expired run should explain why")
	}
	// 关键不变量：单元不再被占用。
	if _, ok, _ := db.FindActiveRun(ctx, snap.Summary.ID, domain.TaskOutline, unitID); ok {
		t.Fatal("expired run still blocks the unit")
	}
}

// 回归：历史遗留的「候选已终结、run 仍 waiting_accept」必须能自愈。
//
// 这类僵局没有任何界面出口：候选不在待审列表里，接受会报过期，拒绝找不到入口。
// 自愈放在 Start 里，因为那是作者唯一能主动触发的动作。
func TestStartReapsRunWhoseCandidateIsAlreadyClosed(t *testing.T) {
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
	ctx := context.Background()

	root := filepath.Join(home, "book")
	src, _ := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan"))
	copyBookDir(t, src, root)
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	unitID := snap.Units[0].UnitID
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		candState domain.CandidateStatus
		wantRun   domain.RunStatus
	}{
		{"expired", domain.CandidateExpired, domain.RunExpired},
		{"rejected", domain.CandidateRejected, domain.RunRejected},
		{"accepted", domain.CandidateAccepted, domain.RunSucceeded},
	}
	for _, tc := range cases {
		runID := "run-" + tc.name
		candID := "cand-" + tc.name
		run := domain.Run{
			ID: runID, BookID: snap.Summary.ID, Kind: domain.TaskOutline, Status: domain.RunWaiting,
			CandidateID: candID,
			Spec:        domain.TaskSpec{Kind: domain.TaskOutline, BookID: snap.Summary.ID, UnitID: unitID},
			CreatedAt:   time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := db.SaveRun(ctx, run); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveCandidate(ctx, domain.Candidate{
			ID: candID, RunID: runID, BookID: snap.Summary.ID, Kind: domain.TaskOutline,
			Status: tc.candState, BaseRevision: snap.Revision, UnitID: unitID,
		}); err != nil {
			t.Fatal(err)
		}

		svc := New(db, settings, llm.NewMock())
		reaped := svc.reapIfCandidateClosed(ctx, run)
		if !reaped {
			t.Fatalf("%s: orphaned run was not reaped", tc.name)
		}
		got, err := db.GetRun(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != tc.wantRun {
			t.Fatalf("%s: run status=%s want %s", tc.name, got.Status, tc.wantRun)
		}
		if _, ok, _ := db.FindActiveRun(ctx, snap.Summary.ID, domain.TaskOutline, unitID); ok {
			t.Fatalf("%s: run still occupies the unit", tc.name)
		}
	}
}

// 候选仍 open 时自愈不能越权：那条 run 是真实占用，必须继续挡住新调用。
func TestReapLeavesLiveCandidateAlone(t *testing.T) {
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
	ctx := context.Background()

	run := domain.Run{
		ID: "run-live", BookID: "b", Kind: domain.TaskOutline, Status: domain.RunWaiting, CandidateID: "c-live",
		Spec: domain.TaskSpec{Kind: domain.TaskOutline, BookID: "b", UnitID: "v01-U1"},
	}
	if err := db.SaveRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveCandidate(ctx, domain.Candidate{
		ID: "c-live", RunID: run.ID, BookID: "b", Kind: domain.TaskOutline, Status: domain.CandidateOpen,
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(db, settings, llm.NewMock())
	if svc.reapIfCandidateClosed(ctx, run) {
		t.Fatal("open candidate must keep its run")
	}
	if got, _ := db.GetRun(ctx, run.ID); got.Status != domain.RunWaiting {
		t.Fatalf("live run was closed: %s", got.Status)
	}
}
