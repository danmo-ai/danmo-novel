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

// 默认（前端不传 followUpReview）接受修订就是交付终点：落盘、归档，不再起审校。
//
// 实测同一单元连续 6 轮审校，复审报出的"新问题"有 96%+ 落在这次没改过的原文上，
// 是前几轮没扫到的旧账。自动复审会让作者分不清"修好了"和"又发现一批旧的"，
// 并把调用成本翻倍。想再查请手动点「重审」。
func TestAcceptReviseStopsAtWritebackByDefault(t *testing.T) {
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

	reviewRun := domain.Run{
		ID: "run-review-author", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.RunWaiting,
		Spec: domain.TaskSpec{Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID},
	}
	if err := db.SaveRun(ctx, reviewRun); err != nil {
		t.Fatal(err)
	}
	review := domain.Candidate{
		ID: "review-author", RunID: reviewRun.ID, BookID: snap.Summary.ID, Kind: domain.TaskReview,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{{ID: "f1", Severity: "blocking", Chapter: 1, Title: "节奏太平"}},
	}
	revise := domain.Candidate{
		ID: "revise-author", BookID: snap.Summary.ID, Kind: domain.TaskRevise, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		BaseChapters: []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "原正文"}},
		Chapters:     []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "修订后的正文"}},
	}
	for _, c := range []domain.Candidate{review, revise} {
		if err := db.SaveCandidate(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	mock := llm.NewMock()
	svc := New(db, settings, mock)
	got, err := svc.Accept(ctx, domain.AcceptCommand{
		CandidateID: "revise-author", BaseRevision: snap.Revision,
		// 不传 FollowUpReview：这就是前端的默认行为。
	})
	if err != nil {
		t.Fatalf("accept revise: %v", err)
	}
	if got.Status != domain.CandidateAccepted {
		t.Fatalf("status=%s", got.Status)
	}
	if got.FollowUpRunID != "" {
		t.Fatalf("no follow-up review must start by default, got run %s", got.FollowUpRunID)
	}
	// 一次 LLM 调用都不该发生。
	if len(mock.Requests) != 0 {
		t.Fatalf("accepting a revision must not call the model, got %d calls", len(mock.Requests))
	}
	// 旧清单必须保持打开，不能归档。
	//
	// 作者通常只勾选一部分问题来修（实测一次审校 30+ 条，勾 5 条很常见）。落盘后
	// 剩下那些仍未修，清单若被归档成「已通过」，就是谎报已处理；留成 open 才能
	// 继续勾选下一批，最后靠「通过 / 强制通过」显式收尾。
	stillOpen, err := db.GetCandidate(ctx, "review-author")
	if err != nil {
		t.Fatal(err)
	}
	if stillOpen.Status != domain.CandidateOpen {
		t.Fatalf("remaining review list status=%s want open (unfixed findings must stay actionable)",
			stillOpen.Status)
	}
	// 正文落盘。
	after, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after.Units[0].ProseRaw, "修订后的正文") {
		t.Fatal("revision was not written back")
	}
	// 没有**新增**审校 run：原来那条（产出了仍打开的清单）本来就该继续活跃。
	runsAfter, err := db.ListRuns(ctx, snap.Summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reviews int
	for _, r := range runsAfter {
		if r.Kind == domain.TaskReview {
			reviews++
		}
	}
	if reviews != 1 {
		t.Fatalf("review runs=%d want 1 (the original list's run, no new one)", reviews)
	}
}

// 审校结案是清单的第三个出口：只有"去修订/拒绝"时，一份 0 阻断的清单既不能
// 归档也不能前进，单元会永远停在 drafted。
func TestCloseUnitReviewRefusesBlockingThenForces(t *testing.T) {
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
	if err := db.SaveCandidate(ctx, review); err != nil {
		t.Fatal(err)
	}

	svc := New(db, settings, llm.NewMock())
	// 不带 force：必须拒绝，且清单保持 open——静默放行会让"没路了"变成"混过去"。
	if _, err := svc.CloseUnitReview(ctx, snap.Summary.ID, unitID, false, false); err == nil {
		t.Fatal("blocking list must not close without force")
	}
	if still, err := db.GetCandidate(ctx, "review-1"); err != nil || still.Status != domain.CandidateOpen {
		t.Fatalf("list should stay open, got %+v %v", still.Status, err)
	}
	// 带 force：归档，并把它那条 waiting_accept 的 run 收尾。
	res, err := svc.CloseUnitReview(ctx, snap.Summary.ID, unitID, true, false)
	if err != nil {
		t.Fatalf("force close: %v", err)
	}
	if res.Closed != 1 || res.Verdict != domain.ReviewBlocking {
		t.Fatalf("result=%+v want closed=1 verdict=blocking", res)
	}
	archived, err := db.GetCandidate(ctx, "review-1")
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != domain.CandidateAccepted {
		t.Fatalf("forced close should archive, got %s", archived.Status)
	}
	if r, err := db.GetRun(ctx, reviewRun.ID); err == nil && r.Status == domain.RunWaiting {
		t.Fatalf("archived review run still waiting_accept; blocks the finalize run")
	}
}

// 0 阻断 ≠ 可以过：截断/分章失败留下的标记让清单变成 unfinished，
// 否则"第 2 章没审完"会被读成"第 2 章没问题"。
func TestReviewGateTreatsTruncationAsUnfinished(t *testing.T) {
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
	cand := domain.Candidate{
		ID: "review-trunc", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{
			{ID: domain.FindingReviewTruncated, Severity: "advisory", Chapter: 2, Title: "审校输出被截断"},
		},
	}
	if err := db.SaveCandidate(ctx, cand); err != nil {
		t.Fatal(err)
	}
	svc := New(db, settings, llm.NewMock())
	if v, n := svc.ReviewGate(ctx, snap.Summary.ID, unitID); n != 1 || v != domain.ReviewUnfinished {
		t.Fatalf("gate=%q lists=%d, want unfinished/1", v, n)
	}
	if _, err := svc.CloseUnitReview(ctx, snap.Summary.ID, unitID, false, false); err == nil {
		t.Fatal("unfinished review must not close without force")
	}
}

// 干净清单结案后直接起定稿，"下一步"才能从审校推进到定稿。
func TestCloseUnitReviewStartsFinalize(t *testing.T) {
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
	// 夹具只有细纲；定稿要求正文已存在，所以先写一章。
	if err := books.WriteUnitProse(root, "v01-U1", []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "正文"}}); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	unitID := snap.Units[0].UnitID
	if u, ok := findUnit(snap, unitID); !ok || !u.HasProse {
		t.Fatalf("unit %s should have prose after write", unitID)
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		ID: "review-clean", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{{ID: "a1", Severity: "advisory", Chapter: 1, Title: "比喻重复"}},
	}
	if err := db.SaveCandidate(ctx, cand); err != nil {
		t.Fatal(err)
	}

	svc := New(db, settings, llm.NewMock())
	if v, n := svc.ReviewGate(ctx, snap.Summary.ID, unitID); n != 1 || v != domain.ReviewClean {
		t.Fatalf("gate=%q lists=%d, want clean/1", v, n)
	}
	res, err := svc.CloseUnitReview(ctx, snap.Summary.ID, unitID, false, true)
	if err != nil {
		t.Fatalf("close clean review: %v", err)
	}
	if res.Closed != 1 || res.Verdict != domain.ReviewClean {
		t.Fatalf("result=%+v want closed=1 verdict=clean", res)
	}
	if res.RunID == "" {
		t.Fatalf("finalize run was not started: %+v", res)
	}
	run, err := db.GetRun(ctx, res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Kind != domain.TaskFinalize || run.Spec.UnitID != unitID {
		t.Fatalf("follow-up run=%s unit=%s, want finalize %s", run.Kind, run.Spec.UnitID, unitID)
	}
	// 等定稿跑完，避免测试结束后 goroutine 还在写临时库。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r, err := db.GetRun(ctx, res.RunID); err == nil && r.Status != domain.RunRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 重审必须能覆盖未处理的旧清单：那条 waiting_accept 的 run 会一直占着单元，
// 而审校清单在界面上没有"接受"按钮——不放行就只能靠作者手动处置，等于死结。
func TestSupersedeReviewArchivesOpenListAndStarts(t *testing.T) {
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
	if err := books.WriteUnitProse(root, "v01-U1", []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "正文"}}); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	unitID := snap.Units[0].UnitID
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	oldRun := domain.Run{
		ID: "run-review-old", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.RunWaiting,
		Spec:      domain.TaskSpec{Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.SaveRun(ctx, oldRun); err != nil {
		t.Fatal(err)
	}
	old := domain.Candidate{
		ID: "review-old", RunID: oldRun.ID, BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{{ID: "f1", Severity: "blocking", Chapter: 1, Title: "节奏太平"}},
	}
	if err := db.SaveCandidate(ctx, old); err != nil {
		t.Fatal(err)
	}

	svc := New(db, settings, llm.NewMock())
	// 不带 supersede：必须报冲突，且提示要指向界面里真实存在的按钮。
	_, err = svc.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID, SplitByChapter: true,
	})
	if err == nil {
		t.Fatal("review with an open list must conflict without supersedeReview")
	}
	if !strings.Contains(err.Error(), "通过") {
		t.Fatalf("conflict hint must name the real actions (通过/强制通过/拒绝), got: %v", err)
	}

	// 带 supersede：旧清单归档、旧 run 收尾，新审校起来了。
	run, err := svc.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID,
		SplitByChapter: true, SupersedeReview: true,
	})
	if err != nil {
		t.Fatalf("supersede review: %v", err)
	}
	if run.ID == "" || run.ID == oldRun.ID {
		t.Fatalf("expected a new review run, got %q", run.ID)
	}
	archived, err := db.GetCandidate(ctx, "review-old")
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != domain.CandidateAccepted {
		t.Fatalf("old list should be archived, got %s", archived.Status)
	}
	if r, err := db.GetRun(ctx, oldRun.ID); err == nil && r.Status == domain.RunWaiting {
		t.Fatal("old review run still waiting_accept; it would keep blocking the unit")
	}
}

// 复审必须是显式请求：不带 FollowUpReview 时 Accept 只落盘，不再自己起一轮审校。
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
		t.Fatalf("accept without FollowUpReview must not start a second review, got run %s", got.FollowUpRunID)
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
