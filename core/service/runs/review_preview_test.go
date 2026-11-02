package runs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// 确认弹窗里的上下文预览必须与真正会发出的调用一致。以前预览固定给"整单元"
// 那一份：作者开着分章审校，弹窗却显示一份塞满整份正文的包和 2 次预算，
// 实际跑的是每章一次、预算 = 章数×2。
func TestReviewPreviewMatchesActualMode(t *testing.T) {
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
	prose := "## 第1章 夜\n\n正文一\n\n## 第2章 门\n\n正文二\n\n## 第3章 影\n\n正文三"
	if err := os.MkdirAll(filepath.Join(root, "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "units", "v01-U1.md"), []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	svc := New(db, settings, nil)
	unitID := snap.Units[0].UnitID

	proseBlock := func(p domain.ContextPacket) string {
		payload, err := contextsvc.DecodeUserPayload(p.UserPrompt)
		if err != nil {
			t.Fatalf("decode user payload: %v", err)
		}
		for _, d := range payload.Documents {
			if d.ID == "prose" {
				return d.Content
			}
		}
		return ""
	}

	// 默认设置 = 分章审校：预览只带第 1 章，预算按章数算。
	if !settings.Get().Review.SplitByChapter {
		t.Fatal("default should be split review")
	}
	packet, err := svc.PreviewPacket(ctx, domain.TaskSpec{Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got := proseBlock(packet); got != "正文一" {
		t.Fatalf("split preview should carry only chapter 1, got %q", got)
	}
	if packet.Policy.MaxCalls != 3*2 {
		t.Fatalf("split preview maxCalls=%d, want 6 (3 chapters × 2)", packet.Policy.MaxCalls)
	}

	// 关掉分章：预览切换成整单元，正文是整份。
	_ = settings.Update(func(s *config.Settings) { s.Review.SplitByChapter = false })
	packet, err = svc.PreviewPacket(ctx, domain.TaskSpec{Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got := proseBlock(packet); got != prose {
		t.Fatalf("whole-unit preview should carry the full unit, got %q", got)
	}
	if packet.Policy.MaxCalls != 2 {
		t.Fatalf("whole-unit preview maxCalls=%d, want 2", packet.Policy.MaxCalls)
	}

	// spec 可以强制分章（审校页「重审该单元」），不受设置影响。
	packet, err = svc.PreviewPacket(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID, SplitByChapter: true,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got := proseBlock(packet); got != "正文一" {
		t.Fatalf("forced split preview should carry only chapter 1, got %q", got)
	}
	if !svc.reviewSplit(domain.TaskSpec{SplitByChapter: true}) {
		t.Fatal("spec.SplitByChapter must force split review even when settings say otherwise")
	}
}

// 开关必须真的改变"调用几次"：分章 = 每章一次，整单元 = 一次。只看预览不够，
// 这里跑真实 run，用假模型数调用。
func TestReviewSwitchDecidesCallCount(t *testing.T) {
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
	prose := "## 第1章 夜\n\n正文一\n\n## 第2章 门\n\n正文二\n\n## 第3章 影\n\n正文三"
	if err := os.MkdirAll(filepath.Join(root, "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "units", "v01-U1.md"), []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	unitID := snap.Units[0].UnitID
	pass := `{"verdict":"pass","findings":[]}`

	// 整单元：一次调用就够。轮数固定 1，单独测"分章 vs 整单元"这一个变量。
	_ = settings.Update(func(s *config.Settings) { s.Review.Rounds = 1 })
	mock := llm.NewMock().AddText(pass)
	svc := New(db, settings, mock)
	_ = settings.Update(func(s *config.Settings) { s.Review.SplitByChapter = false })
	runWhole, err := svc.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID, BaseRevision: snap.Revision,
	})
	if err != nil {
		t.Fatalf("start whole-unit review: %v", err)
	}
	if !waitRunSettled(t, db, runWhole.ID, 5*time.Second) {
		t.Fatal("whole-unit review did not finish")
	}
	if len(mock.Requests) != 1 {
		t.Fatalf("whole-unit review calls=%d, want 1", len(mock.Requests))
	}

	// 分章：每章一次。
	_ = settings.Update(func(s *config.Settings) { s.Review.SplitByChapter = true })
	mock2 := llm.NewMock()
	for i := 0; i < 3; i++ {
		mock2.AddText(pass)
	}
	svc2 := New(db, settings, mock2)
	// 上一轮产出的清单会挡住新的审校；重审语义会先归档它。
	runSplit, err := svc2.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unitID, BaseRevision: snap.Revision,
		SupersedeReview: true,
	})
	if err != nil {
		t.Fatalf("start split review: %v", err)
	}
	if !waitRunSettled(t, db, runSplit.ID, 5*time.Second) {
		t.Fatal("split review did not finish")
	}
	if len(mock2.Requests) != 3 {
		t.Fatalf("split review calls=%d, want 3 (one per chapter at rounds=1)", len(mock2.Requests))
	}
}

// 轮数必须真的乘上调用数：这是提高覆盖率的代价，也是预算申报的依据。
//
// 单轮覆盖率约 31%，是抽样不确定性（同一正文跑 4 轮、并集 128 处、0 处被四轮
// 全中），所以默认 3 轮。测试固定 2 轮 × 3 章 = 6 次，确保轮数没被忽略。
func TestReviewRoundsMultiplyCalls(t *testing.T) {
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
	prose := "## 第1章 夜\n\n正文一\n\n## 第2章 门\n\n正文二\n\n## 第3章 影\n\n正文三"
	if err := os.MkdirAll(filepath.Join(root, "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "units", "v01-U1.md"), []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}

	_ = settings.Update(func(s *config.Settings) {
		s.Review.SplitByChapter = true
		s.Review.Rounds = 2
	})
	mock := llm.NewMock()
	for i := 0; i < 6; i++ {
		mock.AddText(`{"verdict":"pass","findings":[]}`)
	}
	svc := New(db, settings, mock)
	run, err := svc.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: snap.Units[0].UnitID, BaseRevision: snap.Revision,
	})
	if err != nil {
		t.Fatalf("start review: %v", err)
	}
	if !waitRunSettled(t, db, run.ID, 10*time.Second) {
		t.Fatal("review did not finish")
	}
	if len(mock.Requests) != 6 {
		t.Fatalf("calls=%d, want 6 (3 chapters × 2 rounds)", len(mock.Requests))
	}
	// 预算按「章数 × 轮数 × 2（审校 + 一次结构修复）」申报，否则 UI 会显示越界。
	got, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Policy.MaxCalls != 3*2*2 {
		t.Fatalf("maxCalls=%d, want 12 (3 chapters × 2 rounds × 2)", got.Policy.MaxCalls)
	}
}
