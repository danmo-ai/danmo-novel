package runs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// 知识文章/审校清单/去 AI 味规则是"本次 run"的设置。曾经它们被写回共享的
// pipeline.Runner，于是并发的另一个 run 会把它们改掉甚至清空：一个正在跑的
// 审校会在重试时突然丢掉审校清单。这里用一个受控的假模型把顺序钉死。
func TestConcurrentRunsKeepTheirOwnKnowledge(t *testing.T) {
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
	// 夹具只有 v01-U1；补一份 v01-U2 的合同，让两个单元能各自审校。
	u2 := "unit_id: v01-U2\nvolume: 1\nindex: 2\nchapter_from: 2\nchapter_to: 2\nword_target: \"2500\"\nstatus: accepted\n"
	if err := os.WriteFile(filepath.Join(root, "outline", "units", "v01-U2.yaml"), []byte(u2), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"v01-U1", "v01-U2"} {
		if err := books.WriteUnitProse(root, id, []domain.ChapterDraft{{Chapter: 1, Title: "一", Body: "正文正文正文"}}); err != nil {
			t.Fatal(err)
		}
		if err := books.SetUnitStatus(root, id, domain.UnitDrafted); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(ctx, snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}

	llm := &gatedReviewLLM{
		firstU1: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := New(db, settings, llm)

	// A：带参考知识的审校，首次输出故意不合规，逼出一次 repair 重试——
	// 重试发生在 B 结束之后，正是"共享 Runner 被改掉"会露馅的时刻。
	runA, err := svc.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: "v01-U1",
		BaseRevision: snap.Revision,
		KnowledgeIDs: []string{"01-pacing-structure"},
	})
	if err != nil {
		t.Fatalf("start A: %v", err)
	}
	select {
	case <-llm.firstU1:
	case <-time.After(5 * time.Second):
		t.Fatal("A never reached the model")
	}

	// B：同一本书另一个单元，不带知识。它跑完会把"本次的知识"清掉（旧实现里
	// 清的是共享字段）。
	runB, err := svc.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: "v01-U2",
		BaseRevision: snap.Revision,
	})
	if err != nil {
		t.Fatalf("start B: %v", err)
	}
	if !waitRunSettled(t, db, runB.ID, 5*time.Second) {
		t.Fatal("B did not finish")
	}

	close(llm.release)
	if !waitRunSettled(t, db, runA.ID, 5*time.Second) {
		t.Fatal("A did not finish")
	}

	got, err := db.GetRun(ctx, runA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == domain.RunCancelled {
		t.Fatalf("A cancelled: %s", got.Error)
	}
	llm.mu.Lock()
	refs := llm.u1RetryRefs
	total := llm.u1Calls
	llm.mu.Unlock()
	if total < 2 {
		t.Fatalf("U1 calls=%d, want 2 (first + repair)", total)
	}
	if refs == 0 {
		t.Fatalf("U1 repair call lost its reference knowledge: another run's settings leaked in")
	}
}

// gatedReviewLLM 让 v01-U1 的第一次调用卡住，并在重试时记录 payload 里是否
// 还带着作者附加的参考知识。
type gatedReviewLLM struct {
	mu          sync.Mutex
	u1Calls     int
	u1RetryRefs int
	firstU1     chan struct{}
	release     chan struct{}
	firstOnce   sync.Once
}

const passJSON = `{"verdict":"pass","findings":[]}`

func (g *gatedReviewLLM) Chat(ctx context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	payload, err := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
	if err != nil {
		return port.LLMChatResponse{Content: passJSON, Done: true}, nil
	}
	unit, _ := payload.Book["unitId"].(string)
	if unit != "v01-U1" {
		return port.LLMChatResponse{Content: passJSON, Done: true}, nil
	}
	g.mu.Lock()
	g.u1Calls++
	n := g.u1Calls
	if n > 1 {
		g.u1RetryRefs = len(payload.References)
	}
	g.mu.Unlock()

	if n == 1 {
		g.firstOnce.Do(func() { close(g.firstU1) })
		select {
		case <-g.release:
		case <-ctx.Done():
			return port.LLMChatResponse{}, ctx.Err()
		}
		// 首次输出不是 JSON：触发一次 repair 重试。
		return port.LLMChatResponse{Content: "不是 JSON", Done: true}, nil
	}
	return port.LLMChatResponse{Content: passJSON, Done: true}, nil
}

// waitRunSettled 等 run 离开 pending/running。waiting_accept 也算结束——
// 它已经产出候选，后面的 repair 不会再发生。
func waitRunSettled(t *testing.T, db *sqlite.Store, runID string, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if r, err := db.GetRun(context.Background(), runID); err == nil {
			switch r.Status {
			case domain.RunPending, domain.RunRunning:
			default:
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
