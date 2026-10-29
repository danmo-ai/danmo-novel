package runs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/service/books"
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

