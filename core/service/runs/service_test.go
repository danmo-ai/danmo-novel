package runs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

func TestAcceptGuardsEmptyAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock := llm.NewMock()
	models := llmconfig.NewModelRegistry()
	mgr := llmconfig.NewManager(db, models)
	_ = mgr
	svc := New(db, settings, mock)

	// read-only book
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	abs, _ := filepath.Abs(root)
	snap, err := books.LoadBook(abs, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(context.Background(), snap.Summary.ID, abs, snap.Summary.Title, snap.Revision, true); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{ID: "c1", BookID: snap.Summary.ID, Kind: domain.TaskWrite, Status: domain.CandidateOpen, BaseRevision: snap.Revision, UnitID: "v01-U1", Chapters: []domain.ChapterDraft{{Chapter: 1, Body: "x"}}}
	if err := db.SaveCandidate(context.Background(), cand); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(context.Background(), domain.AcceptCommand{CandidateID: "c1", BaseRevision: snap.Revision}); err == nil {
		t.Fatal("read-only accept should fail")
	}
	_ = svc
}

// 进程重启后内存里的 goroutine 消失：遗留的 pending/running 必须被收尾，
// 否则 UI 会一直轮询「运行中」，同单元的新任务也会被 FindActiveRun 挡住。
func TestRecoverInterruptedFailsOrphanedRuns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db, settings, llm.NewMock())
	ctx := context.Background()
	now := time.Now().UTC()
	mk := func(id, unitID string, status domain.RunStatus) domain.Run {
		return domain.Run{
			ID: id, BookID: "b-origin", Kind: domain.TaskWrite, Status: status,
			Spec:      domain.TaskSpec{Kind: domain.TaskWrite, BookID: "b-origin", UnitID: unitID},
			CreatedAt: now, UpdatedAt: now,
		}
	}
	for _, r := range []domain.Run{
		mk("orphan-running", "v01-U1", domain.RunRunning),
		mk("orphan-pending", "v01-U1", domain.RunPending),
		mk("waiting-accept", "v01-U2", domain.RunWaiting),
	} {
		if err := db.SaveRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	n, err := svc.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("recovered=%d, want 2", n)
	}
	for _, id := range []string{"orphan-running", "orphan-pending"} {
		run, err := db.GetRun(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != domain.RunFailed {
			t.Fatalf("%s status=%s, want failed", id, run.Status)
		}
		if run.Error == "" {
			t.Fatalf("%s should carry a restart explanation", id)
		}
	}
	waiting, err := db.GetRun(ctx, "waiting-accept")
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != domain.RunWaiting {
		t.Fatalf("waiting_accept must be untouched, got %s", waiting.Status)
	}

	// 收尾后同单元允许重新发起。
	if _, ok, err := db.FindActiveRun(ctx, "b-origin", domain.TaskWrite, "v01-U1"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("orphaned runs still block new runs")
	}
}
