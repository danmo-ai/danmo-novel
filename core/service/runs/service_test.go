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
	if err := books.SetUnitStatus(root, unitID, domain.UnitDrafted); err != nil {
		t.Fatal(err)
	}
	snap, err = books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := books.SetUnitStatus(root, unitID, domain.UnitDrafted); err != nil {
		t.Fatal(err)
	}
	snap, err = books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
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
	// 修订 run 只是为了让 Accept 知道「这次修了 f1」——IssueIDs 是唯一可信来源，
	// 不能靠比 findings 的标题或锚点去猜。
	reviseRun := domain.Run{
		ID: "run-revise-author", BookID: snap.Summary.ID, Kind: domain.TaskRevise, Status: domain.RunWaiting,
		Spec: domain.TaskSpec{Kind: domain.TaskRevise, BookID: snap.Summary.ID, UnitID: unitID,
			IssueIDs: []string{"f1"}},
	}
	if err := db.SaveRun(ctx, reviseRun); err != nil {
		t.Fatal(err)
	}
	review := domain.Candidate{
		ID: "review-author", RunID: reviewRun.ID, BookID: snap.Summary.ID, Kind: domain.TaskReview,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{
			{ID: "f1", Severity: "blocking", Chapter: 1, Title: "节奏太平"},
			{ID: "f2", Severity: "advisory", Chapter: 1, Title: "措辞重复"},
		},
	}
	revise := domain.Candidate{
		ID: "revise-author", RunID: reviseRun.ID, BookID: snap.Summary.ID, Kind: domain.TaskRevise,
		Status:       domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: unitID,
		BaseChapters: []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "原正文"}, {Chapter: 2, Title: "二", Body: "第二章保留"}, {Chapter: 3, Title: "三", Body: "第三章保留"}},
		Chapters:     []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "修订后的正文"}, {Chapter: 2, Title: "二", Body: "第二章保留"}, {Chapter: 3, Title: "三", Body: "第三章保留"}},
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
	})
	if err != nil {
		t.Fatalf("accept revise: %v", err)
	}
	if got.Status != domain.CandidateAccepted {
		t.Fatalf("status=%s", got.Status)
	}
	// 一次 LLM 调用都不该发生。
	if len(mock.Requests) != 0 {
		t.Fatalf("accepting a revision must not call the model, got %d calls", len(mock.Requests))
	}
	// 旧清单里只剩仍未被修的条目，不能整份归档。
	//
	// 作者通常只勾一部分来修（一次审校 30+ 条，勾 5 条很常见）。已修的那几条
	// 必须从清单里消失，否则再点「自动修复」会把它们再修一遍，而「还剩多少」
	// 这个数字永远不动。没修的要留着继续可操作。
	stillOpen, err := db.GetCandidate(ctx, "review-author")
	if err != nil {
		t.Fatal(err)
	}
	if stillOpen.Status != domain.CandidateExpired {
		t.Fatalf("old-prose review must be expired after revision, got %s", stillOpen.Status)
	}
	if len(stillOpen.Findings) != 2 {
		t.Fatal("expired review must retain unresolved findings for audit")
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

// 清单里的问题全被修完时，那份清单必须自己结案，否则它会永远挂着：
// 既没有条目可修，也不能通过（作者只会看到一个 0 条的僵局）。
func TestAcceptReviseArchivesFullyAddressedList(t *testing.T) {
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
	if err := books.SetUnitStatus(root, unitID, domain.UnitDrafted); err != nil {
		t.Fatal(err)
	}
	snap, err = books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}

	reviewRun := domain.Run{
		ID: "run-review-all", BookID: snap.Summary.ID, Kind: domain.TaskReview, Status: domain.RunWaiting,
		Spec: domain.TaskSpec{Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID},
	}
	reviseRun := domain.Run{
		ID: "run-revise-all", BookID: snap.Summary.ID, Kind: domain.TaskRevise, Status: domain.RunWaiting,
		Spec: domain.TaskSpec{Kind: domain.TaskRevise, BookID: snap.Summary.ID, UnitID: unitID,
			IssueIDs: []string{"f1", "f2"}},
	}
	for _, r := range []domain.Run{reviewRun, reviseRun} {
		if err := db.SaveRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	review := domain.Candidate{
		ID: "review-all", RunID: reviewRun.ID, BookID: snap.Summary.ID, Kind: domain.TaskReview,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision, UnitID: unitID,
		Findings: []domain.ReviewFinding{
			{ID: "f1", Severity: "blocking", Chapter: 1, Title: "甲"},
			{ID: "f2", Severity: "advisory", Chapter: 1, Title: "乙"},
		},
	}
	revise := domain.Candidate{
		ID: "revise-all", RunID: reviseRun.ID, BookID: snap.Summary.ID, Kind: domain.TaskRevise,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision, UnitID: unitID,
		BaseChapters: []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "原正文"}, {Chapter: 2, Title: "二", Body: "第二章保留"}, {Chapter: 3, Title: "三", Body: "第三章保留"}},
		Chapters:     []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "修订后的正文"}, {Chapter: 2, Title: "二", Body: "第二章保留"}, {Chapter: 3, Title: "三", Body: "第三章保留"}},
	}
	for _, c := range []domain.Candidate{review, revise} {
		if err := db.SaveCandidate(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	svc := New(db, settings, llm.NewMock())
	if _, err := svc.Accept(ctx, domain.AcceptCommand{
		CandidateID: "revise-all", BaseRevision: snap.Revision,
	}); err != nil {
		t.Fatalf("accept revise: %v", err)
	}

	got, err := db.GetCandidate(ctx, "review-all")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.CandidateExpired {
		t.Fatalf("old-prose list must be expired, not auto-resolved: %s", got.Status)
	}
	if len(got.Findings) != 2 {
		t.Fatal("selected issue IDs must not erase unresolved findings")
	}
	// 它的 run 也必须收尾，否则会挡住该单元的下一次审校。
	if r, err := db.GetRun(ctx, reviewRun.ID); err == nil && r.Status != domain.RunExpired {
		t.Fatal("old review run must expire with its prose version")
	}
}

// 0 阻断 ≠ 可以过：截断/分章失败留下的标记让清单变成 unfinished，
// 否则"第 2 章没审完"会被读成"第 2 章没问题"。
func TestStartWriteRequiresAcceptedOutline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	_ = settings.Update(func(s *config.Settings) {
		s.ModelAssignments = []domain.ModelAssignment{{Role: domain.StageWrite, ModelID: "mock/mock-gpt-4"}}
	})
	db, err := sqlite.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root, err := books.CreateBookFromDraft(home, books.DraftInput{BookID: "gate", Title: "门禁"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "outline", "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outline", "units", "v01-U1.yaml"), []byte("unit_id: v01-U1\nstatus: proposed\nchapter_range: [1, 1]\n"), 0o644); err != nil {
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
	if _, err := svc.Start(context.Background(), domain.TaskSpec{Kind: domain.TaskWrite, BookID: snap.Summary.ID, UnitID: "v01-U1", BaseRevision: snap.Revision}); err == nil {
		t.Fatal("proposed unit must not enter prose generation")
	}
}

func TestFinalizeAcceptRequiresActualReconciliation(t *testing.T) {
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
	root, err := books.CreateBookScaffold(home, "final-gate", "定稿", "玄幻")
	if err != nil {
		t.Fatal(err)
	}
	unitRaw := "unit_id: v01-U1\nstatus: reviewed\nchapter_range: [1, 1]\non_stage: [lin]\npov: lin\n"
	if err := os.WriteFile(filepath.Join(root, "outline", "units", "v01-U1.yaml"), []byte(unitRaw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := books.WriteUnitProse(root, "v01-U1", []domain.ChapterDraft{{Chapter: 1, Body: "实际正文"}}); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(context.Background(), snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	candidate := domain.Candidate{ID: "fin", BookID: snap.Summary.ID, Kind: domain.TaskFinalize, Status: domain.CandidateOpen, BaseRevision: snap.Revision, UnitID: "v01-U1", Chapters: []domain.ChapterDraft{{Chapter: 1, Body: "实际正文"}}}
	if err := db.SaveCandidate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	svc := New(db, settings, llm.NewMock())
	if _, err := svc.Accept(context.Background(), domain.AcceptCommand{CandidateID: "fin", BaseRevision: snap.Revision}); err == nil {
		t.Fatal("missing actual reconciliation must not be accepted")
	}
}

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
	if err := books.SetUnitStatus(root, unitID, domain.UnitDrafted); err != nil {
		t.Fatal(err)
	}
	snap, err = books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := books.SetUnitStatus(root, unitID, domain.UnitDrafted); err != nil {
		t.Fatal(err)
	}
	snap, err = books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
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
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID, BaseRevision: snap.Revision,
	})
	if err == nil {
		t.Fatalf("review with an open list must conflict without supersedeReview")
	}
	if !strings.Contains(err.Error(), "自动修复") || !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("conflict hint must name the real actions (自动修复/拒绝), got: %v", err)
	}

	// 带 supersede：旧清单归档、旧 run 收尾，新审校起来了。
	run, err := svc.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID, BaseRevision: snap.Revision,
		SupersedeReview: true,
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
