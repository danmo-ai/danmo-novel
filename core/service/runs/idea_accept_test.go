package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// 接受创意提案只写 outline/idea.md 并推进 stage，不碰总纲/设定/人物。
func TestAcceptIdeaWritesProposalAndAdvancesStage(t *testing.T) {
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

	root, err := books.CreateBookFromDraft(dir, books.DraftInput{BookID: "idea-book", Title: "雪夜", Genre: "悬疑", Idea: "新警撞见旧案"})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(context.Background(), snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	svc := New(db, settings, llm.NewMock())

	proposal := books.MergeIdeaProposal(snap.Files[domain.IdeaPath], "## 推荐主线\n主角在卷宗里发现自己的名字")
	cand := domain.Candidate{
		ID: "idea-1", BookID: snap.Summary.ID, Kind: domain.TaskIdea, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, TargetPath: domain.IdeaPath, Replacement: proposal,
	}
	if err := db.SaveCandidate(context.Background(), cand); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(context.Background(), domain.AcceptCommand{CandidateID: "idea-1", BaseRevision: snap.Revision}); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(domain.IdeaPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "主角在卷宗里发现自己的名字") || !strings.Contains(string(written), "新警撞见旧案") {
		t.Fatalf("idea.md not merged correctly: %s", written)
	}
	state, _ := os.ReadFile(filepath.Join(root, "novel-state.yaml"))
	if !strings.Contains(string(state), "stage: "+domain.StageIdeaExpanded) {
		t.Fatalf("stage not advanced: %s", state)
	}
	if !strings.Contains(string(state), "next_action: plan") {
		t.Fatalf("next_action not advanced: %s", state)
	}
	// 创意提案不是 canon：总纲不能被写出来。
	if _, err := os.Stat(filepath.Join(root, "outline", "book_outline.md")); err == nil {
		t.Fatal("idea accept must not write the book outline")
	}
}

// 空内容的创意候选不能被接受（否则 idea.md 会被清空）。
func TestAcceptIdeaRejectsEmptyProposal(t *testing.T) {
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
	root, err := books.CreateBookFromDraft(dir, books.DraftInput{BookID: "empty-idea", Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(context.Background(), snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	svc := New(db, settings, llm.NewMock())
	cand := domain.Candidate{ID: "idea-empty", BookID: snap.Summary.ID, Kind: domain.TaskIdea, Status: domain.CandidateOpen, BaseRevision: snap.Revision, TargetPath: domain.IdeaPath}
	if err := db.SaveCandidate(context.Background(), cand); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(context.Background(), domain.AcceptCommand{CandidateID: "idea-empty"}); err == nil {
		t.Fatal("empty idea proposal must not be accepted")
	}
}

// 回归：TaskIdea 走成本较低的 plan 阶段模型。
func TestResolveModelForIdeaUsesPlanRole(t *testing.T) {
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
	mgr := llmconfig.NewManager(db, llmconfig.NewModelRegistry())
	_ = mgr
	svc := New(db, settings, llm.NewMock())
	if err := svc.Settings.Update(func(st *config.Settings) {
		st.ModelAssignments = []domain.ModelAssignment{{Role: domain.StagePlan, ModelID: "mock/plan-model"}}
	}); err != nil {
		t.Fatal(err)
	}
	if got := svc.resolveModel(domain.TaskSpec{Kind: domain.TaskIdea}); got != "mock/plan-model" {
		t.Fatalf("idea should use plan role, got %q", got)
	}
}

// 端到端：Start(kind=idea) 走 RunIdeaExpand 产出候选，预览与执行读到同一份上下文。
func TestStartIdeaProducesProposalCandidate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Update(func(st *config.Settings) {
		st.ModelAssignments = []domain.ModelAssignment{{Role: domain.StagePlan, ModelID: "mock/plan-model"}}
	}); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root, err := books.CreateBookFromDraft(dir, books.DraftInput{BookID: "idea-run", Title: "雪夜", Genre: "悬疑", Idea: "新警撞见旧案"})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(context.Background(), snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	mock := llm.NewMock().AddText(strings.Repeat("## 推荐主线\n主角在卷宗里发现自己的名字。\n", 8))
	svc := New(db, settings, mock)
	run, err := svc.Start(context.Background(), domain.TaskSpec{Kind: domain.TaskIdea, BookID: snap.Summary.ID, BaseRevision: snap.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if !waitRunSettled(t, db, run.ID, 5*time.Second) {
		t.Fatal("idea run did not settle")
	}
	done, _ := db.GetRun(context.Background(), run.ID)
	if done.Status != domain.RunWaiting {
		t.Fatalf("run status=%s err=%s", done.Status, done.Error)
	}
	cand, err := db.GetCandidate(context.Background(), done.CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if cand.Kind != domain.TaskIdea || cand.TargetPath != domain.IdeaPath {
		t.Fatalf("candidate=%+v", cand)
	}
	if !strings.Contains(cand.Replacement, "主角在卷宗里发现自己的名字") {
		t.Fatal("proposal missing")
	}
}
