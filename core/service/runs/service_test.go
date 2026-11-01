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
	"github.com/danmo-ai/danmo-novel/core/service/gates"
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

// 审校候选只承载问题清单：接受=归档，不写盘。正文被定向修订后 revision 已变，
// 旧审校候选仍必须可归档，而不是报 block changed / 变成 expired。
func TestAcceptReviewArchivesAfterProseMovedOn(t *testing.T) {
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
	if len(snap.Units) == 0 {
		t.Fatal("fixture book has no units")
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		ID: "review-1", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.CandidateOpen,
		BaseRevision: "revision-before-revise", UnitID: snap.Units[0].UnitID,
		Findings: []domain.ReviewFinding{{ID: "f1", Severity: "blocking", Title: "节奏太平"}},
	}
	if err := db.SaveCandidate(ctx, cand); err != nil {
		t.Fatal(err)
	}

	svc := New(db, settings, llm.NewMock())
	got, err := svc.Accept(ctx, domain.AcceptCommand{CandidateID: "review-1", BaseRevision: snap.Revision})
	if err != nil {
		t.Fatalf("accept review should archive regardless of revision: %v", err)
	}
	if got.Status != domain.CandidateAccepted {
		t.Fatalf("status=%s, want accepted", got.Status)
	}

	// 对照：正文候选仍必须校验 revision，避免用旧稿覆盖新稿。
	stale := domain.Candidate{
		ID: "write-1", BookID: snap.Summary.ID, Kind: domain.TaskWrite, Status: domain.CandidateOpen,
		BaseRevision: "revision-before-revise", UnitID: snap.Units[0].UnitID,
		Chapters: []domain.ChapterDraft{{Chapter: 1, Body: "x"}},
	}
	if err := db.SaveCandidate(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(ctx, domain.AcceptCommand{CandidateID: "write-1"}); err == nil {
		t.Fatal("stale prose candidate must not be accepted")
	}
}

// 拒绝修订候选 = 丢弃这一次修复：正文不变、审校清单仍在、运行记录标成"已拒绝"
// 而不是"成功"，否则运行记录会显示一切正常，掩盖一次返工。
func TestRejectReviseKeepsProseAndReviewOpen(t *testing.T) {
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
	// 先写一版正文，才能验证"拒绝不改正文"。
	if err := books.WriteUnitProse(root, unitID, []domain.ChapterDraft{{Chapter: 1, Title: "原", Body: "原始正文"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "units", unitID+".md"))

	review := domain.Candidate{
		ID: "review-1", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{{ID: "f1", Severity: "blocking", Chapter: 1, Title: "节奏太平"}},
	}
	reviseRun := domain.Run{
		ID: "run-revise-1", BookID: snap.Summary.ID, Kind: domain.TaskRevise, Status: domain.RunWaiting,
		Spec:      domain.TaskSpec{Kind: domain.TaskRevise, BookID: snap.Summary.ID, UnitID: unitID},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	revise := domain.Candidate{
		ID: "revise-1", RunID: reviseRun.ID, BookID: snap.Summary.ID, Kind: domain.TaskRevise,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision, UnitID: unitID,
		BaseChapters: []domain.ChapterDraft{{Chapter: 1, Title: "原", Body: "原始正文"}},
		Chapters:     []domain.ChapterDraft{{Chapter: 1, Title: "改", Body: "模型改过的正文"}},
	}
	if err := db.SaveRun(ctx, reviseRun); err != nil {
		t.Fatal(err)
	}
	for _, c := range []domain.Candidate{review, revise} {
		if err := db.SaveCandidate(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	svc := New(db, settings, llm.NewMock())
	if err := svc.Reject(ctx, "revise-1", "改得不对"); err != nil {
		t.Fatal(err)
	}

	// 正文逐字不变。
	after, _ := os.ReadFile(filepath.Join(root, "units", unitID+".md"))
	if string(after) != string(before) {
		t.Fatalf("rejecting a revise candidate must not touch prose:\nbefore=%q\nafter=%q", before, after)
	}
	// 候选被丢弃，但审校清单仍可继续用。
	if got, _ := db.GetCandidate(ctx, "revise-1"); got.Status != domain.CandidateRejected {
		t.Fatalf("revise candidate status=%s", got.Status)
	}
	if got, _ := db.GetCandidate(ctx, "review-1"); got.Status != domain.CandidateOpen {
		t.Fatalf("review list should stay open for another attempt, got %s", got.Status)
	}
	// 运行记录标成已拒绝，而不是成功。
	run, err := db.GetRun(ctx, reviseRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != domain.RunRejected {
		t.Fatalf("run status=%s want rejected", run.Status)
	}
	if run.Error != "改得不对" {
		t.Fatalf("run error=%q want the author's reason", run.Error)
	}
	// 被拒绝的 run 不能继续占用该单元的活跃名额。
	if _, ok, err := db.FindActiveRun(ctx, snap.Summary.ID, domain.TaskRevise, unitID); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("rejected run still blocks a new revise")
	}
}

// 去 AI 味程序规则允许覆盖：本书 > 作者机器文件 > 内置。
// 两条覆盖路径都是普通 YAML，不是知识文章——规则表永远不该进提示词。
func TestLoadDeslopRulesPrefersOverrides(t *testing.T) {
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
	root := t.TempDir()

	// 默认：内置规则。
	rs, err := svc.loadDeslopRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != gates.RulesFromBuiltin {
		t.Fatalf("source=%s want builtin", rs.Source)
	}

	// 作者机器文件：<home>/gates/deslop-rules.yaml。
	authorDir := filepath.Join(home, "gates")
	if err := os.MkdirAll(authorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	authorRules := "signals:\n  - id: authoronly\n    title: 作者规则\n    severity: advisory\n    pattern: '作者专属'\n"
	if err := os.WriteFile(filepath.Join(authorDir, "deslop-rules.yaml"), []byte(authorRules), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err = svc.loadDeslopRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != gates.RulesFromAuthor || rs.Signals[0].ID != "authoronly" {
		t.Fatalf("author override lost: source=%s", rs.Source)
	}

	// 本书文件优先于作者机器文件。
	if err := os.MkdirAll(filepath.Join(root, "canon"), 0o755); err != nil {
		t.Fatal(err)
	}
	bookRules := "signals:\n  - id: bookonly\n    title: 本书规则\n    severity: advisory\n    pattern: '本书专属'\n"
	if err := os.WriteFile(filepath.Join(root, "canon", "deslop-rules.yaml"), []byte(bookRules), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err = svc.loadDeslopRules(root)
	if err != nil {
		t.Fatal(err)
	}
	if rs.Source != gates.RulesFromBook || rs.Signals[0].ID != "bookonly" {
		t.Fatalf("book override should win: source=%s", rs.Source)
	}

	// 写错的正则必须报错，不能静默退回内置。
	broken := "signals:\n  - id: bad\n    title: 坏规则\n    severity: advisory\n    pattern: '('\n"
	if err := os.WriteFile(filepath.Join(authorDir, "deslop-rules.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.loadDeslopRules(t.TempDir()); err == nil {
		t.Fatal("broken author rule must surface as an error")
	}
}

func copyBookDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// 闭环一：接受修订候选（FollowUpReview）后，该单元已处理的审校清单必须归档，
// 并自动起一次分章复审，让作者看到还剩下什么，而不是一份永远待处理的旧清单。
func TestAcceptReviseArchivesFindingsAndStartsFollowUpReview(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	_ = settings.Update(func(s *config.Settings) {
		s.ModelAssignments = []domain.ModelAssignment{{Role: domain.StageReview, ModelID: "mock/mock-gpt-4"}}
	})
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

	// 一份待处理审校清单 + 一份修订候选（修订只改第 1 章，其余原样）。
	// 给审校清单配一条 waiting_accept 的 run：它若不收尾，会挡住下面的复审启动。
	reviewRun := domain.Run{
		ID: "run-review-1", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.RunWaiting,
		Spec:      domain.TaskSpec{Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.SaveRun(ctx, reviewRun); err != nil {
		t.Fatal(err)
	}
	review := domain.Candidate{
		ID: "review-1", RunID: reviewRun.ID, BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{{ID: "f1", Severity: "blocking", Chapter: 1, Title: "节奏太平"}},
	}
	revise := domain.Candidate{
		ID: "revise-1", BookID: snap.Summary.ID, Kind: domain.TaskRevise, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		Chapters: []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "修订后的正文"}},
	}
	for _, c := range []domain.Candidate{review, revise} {
		if err := db.SaveCandidate(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	svc := New(db, settings, llm.NewMock())
	got, err := svc.Accept(ctx, domain.AcceptCommand{
		CandidateID: "revise-1", BaseRevision: snap.Revision, FollowUpReview: true,
	})
	if err != nil {
		t.Fatalf("accept revise: %v", err)
	}
	if got.Status != domain.CandidateAccepted {
		t.Fatalf("status=%s", got.Status)
	}

	// 旧清单归档。
	archived, err := db.GetCandidate(ctx, "review-1")
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != domain.CandidateAccepted {
		t.Fatalf("handled review list should be archived, got %s", archived.Status)
	}
	// 它的 run 也必须收尾，否则会挡住复审（FindActiveRun 视 waiting_accept 为活跃）。
	if r, err := db.GetRun(ctx, reviewRun.ID); err == nil && r.Status == domain.RunWaiting {
		t.Fatalf("archived review run still waiting_accept; blocks the follow-up start")
	}
	// 复审已启动。
	if got.FollowUpRunID == "" {
		t.Fatal("follow-up review run was not started")
	}
	run, err := db.GetRun(ctx, got.FollowUpRunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Kind != domain.TaskReview || !run.Spec.SplitByChapter {
		t.Fatalf("follow-up run=%s split=%v, want split review", run.Kind, run.Spec.SplitByChapter)
	}
}

// 闭环二不受影响：chain 自己驱动复审，不能要求 Accept 再起一次。
func TestAcceptWithoutFollowUpLeavesReviewListOpen(t *testing.T) {
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
	review := domain.Candidate{
		ID: "review-1", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{{ID: "f1", Severity: "blocking", Chapter: 1, Title: "节奏太平"}},
	}
	revise := domain.Candidate{
		ID: "revise-1", BookID: snap.Summary.ID, Kind: domain.TaskRevise, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		Chapters: []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "修订后的正文"}},
	}
	for _, c := range []domain.Candidate{review, revise} {
		if err := db.SaveCandidate(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	svc := New(db, settings, llm.NewMock())
	got, err := svc.Accept(ctx, domain.AcceptCommand{CandidateID: "revise-1", BaseRevision: snap.Revision})
	if err != nil {
		t.Fatalf("accept revise: %v", err)
	}
	if got.FollowUpRunID != "" {
		t.Fatalf("chain path must not start a second review, got run %s", got.FollowUpRunID)
	}
	stillOpen, err := db.GetCandidate(ctx, "review-1")
	if err != nil {
		t.Fatal(err)
	}
	if stillOpen.Status != domain.CandidateOpen {
		t.Fatalf("review list should stay open without FollowUpReview, got %s", stillOpen.Status)
	}
}

// 作者文档即使被显式指定，也不能进提示词。picker 已隐藏它们，
// 这里是执行侧的第二道闸——防止旧前端、MCP 或手写 spec 把它带进来。
func TestLoadKnowledgeSkipsAuthorDocs(t *testing.T) {
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

	got := svc.loadKnowledge([]string{"20-tomato-launch", "90-sources", "22-continuity-information-control"})
	if len(got) != 1 {
		t.Fatalf("loaded %d docs, want only the model-facing one", len(got))
	}
	if !strings.Contains(got[0], "POV") {
		t.Fatalf("wrong doc kept: %q", got[0][:60])
	}
}
