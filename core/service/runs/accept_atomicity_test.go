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
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// 被拒绝的接受绝不能改动磁盘。
//
// 这条曾经是真实故障：Accept 在跑完全部校验之前先调了 books.WriteUnitProse，
// 于是一个带阻断门禁的正文候选虽然返回 400，正文却已经落地。后果是单元进入
// 「有正文、状态还是 accepted」的矛盾态：下一次运行看到 hasProse 就跳过写作，
// 而那条仍为 open 的候选又挡住审校（服务端按单元判活跃），流水线永久卡死。
// 对修订候选更糟——被拒的修订已经覆盖了已定稿的正文，属于静默数据损坏。
func TestRejectedAcceptDoesNotTouchDisk(t *testing.T) {
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
	// 先有一版已落盘的正文，才能验证「被拒的修订不会覆盖它」。
	// 顺序很重要：写正文会改变书籍 revision，必须写在重新 LoadBook 之前，
	// 否则候选的 baseRevision 一上来就不匹配——那样 Accept 会在版本校验处
	// 就失败，根本走不到门禁分支，这个测试也就证明不了任何事。
	if err := books.WriteUnitProse(root, unitID, []domain.ChapterDraft{
		{Chapter: 1, Title: "原", Body: "原始正文，一个字都不该变。"},
	}); err != nil {
		t.Fatal(err)
	}
	snap, err = books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	prosePath := filepath.Join(root, snap.Units[0].ProsePath)
	outlinePath := filepath.Join(root, snap.Units[0].OutlinePath)
	proseBefore, err := os.ReadFile(prosePath)
	if err != nil {
		t.Fatal(err)
	}
	outlineBefore, err := os.ReadFile(outlinePath)
	if err != nil {
		t.Fatal(err)
	}

	run := domain.Run{
		ID: "run-revise-gate", BookID: snap.Summary.ID, Kind: domain.TaskRevise, Status: domain.RunWaiting,
		Spec:      domain.TaskSpec{Kind: domain.TaskRevise, BookID: snap.Summary.ID, UnitID: unitID},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.SaveRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	// 候选本身合规，但带着一条阻断门禁：接受必须失败。
	cand := domain.Candidate{
		ID: "revise-gate", RunID: run.ID, BookID: snap.Summary.ID, Kind: domain.TaskRevise,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision, UnitID: unitID,
		Chapters: []domain.ChapterDraft{{Chapter: 1, Title: "改", Body: "被门禁拦下的新正文。"}},
		Findings: []domain.ReviewFinding{{ID: "deslop-1", Severity: "blocking", Source: "local_gate",
			Lens: "deslop", Chapter: 1, Title: "翻案腔（不是 A 而是 B）"}},
	}
	if err := db.SaveCandidate(ctx, cand); err != nil {
		t.Fatal(err)
	}

	svc := New(db, settings, llm.NewMock())
	if _, err := svc.Accept(ctx, domain.AcceptCommand{CandidateID: "revise-gate"}); err == nil {
		t.Fatal("带阻断门禁的候选必须被拒绝")
	}

	proseAfter, err := os.ReadFile(prosePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(proseAfter) != string(proseBefore) {
		t.Fatalf("被拒绝的接受改动了正文：\nbefore:\n%s\nafter:\n%s", proseBefore, proseAfter)
	}
	outlineAfter, err := os.ReadFile(outlinePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(outlineAfter) != string(outlineBefore) {
		t.Fatalf("被拒绝的接受改动了细纲状态：\nbefore:\n%s\nafter:\n%s", outlineBefore, outlineAfter)
	}
}
