package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"path/filepath"
)

func TestWriteBudgetNoTools(t *testing.T) {
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	snap, err := books.LoadBook(root, true)
	if err != nil {
		t.Fatal(err)
	}
	unit := snap.Units[0]
	// 正文给足字数，避免触发按章修复，验证固定预算 = 章数。
	body := ""
	for i := 0; i < 60; i++ {
		body += "林尘握住刀柄，指节因为用力而微微发白，他没有说话，只是看着试炼场尽头那道缓缓打开的石门。"
	}
	mock := llm.NewMock().
		AddText(`{"chapters":[{"chapter":1,"title":"一","body":"` + body + `"}]}`).
		AddText(`{"chapters":[{"chapter":2,"title":"二","body":"` + body + `"}]}`).
		AddText(`{"chapters":[{"chapter":3,"title":"三","body":"` + body + `"}]}`)
	r := New(mock)
	res, err := r.RunWrite(context.Background(), snap, unit, "", "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Calls) != 3 {
		t.Fatalf("calls=%d want fixed 3 (one per chapter)", len(res.Calls))
	}
	if len(mock.Requests) != 3 {
		t.Fatalf("requests=%d", len(mock.Requests))
	}
	for i, req := range mock.Requests {
		if len(req.Tools) != 0 {
			t.Fatalf("request %d tools must be empty", i)
		}
	}
	if len(mock.Requests[0].Tools) != 0 {
		t.Fatal("tools must be empty")
	}
	if res.Candidate.Status != domain.CandidateOpen {
		t.Fatalf("status=%s", res.Candidate.Status)
	}
	if len(res.Candidate.Chapters) != 3 {
		t.Fatalf("chapters=%d", len(res.Candidate.Chapters))
	}
	// accepting is separate — prose file unchanged
	if strings.TrimSpace(unit.ProseRaw) != "" {
		t.Fatal("fixture should have no prose")
	}
}

func TestRejectToolsAtProvider(t *testing.T) {
	// covered by adapter DefaultLLMProviderClient; smoke via mock path above
}

// 审校只读正文、改不了稿：本地硬门不过时不能靠"再审一次"修复。
// 旧实现在这里会重试一次（多烧调用、丢掉首轮 findings），并把本地 findings 追加两遍。
func TestReviewDoesNotRetryOrDuplicateLocalFindings(t *testing.T) {
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	snap, err := books.LoadBook(root, true)
	if err != nil {
		t.Fatal(err)
	}
	unit := snap.Units[0]
	unit.ProseRaw = "## 第1章\n\n短。" // 字数远低于目标 → 本地硬门判 blocking
	mock := llm.NewMock().AddText(`{"verdict":"pass","findings":[{"id":"ai-1","severity":"advisory","chapter":1,"title":"节奏略平","evidence":"全章只有一句推进"}]}`)
	r := New(mock)
	res, err := r.RunReview(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{Concurrency: 1, Rounds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.Requests) != 1 {
		t.Fatalf("requests=%d want 1（审校重试注定无效，只烧预算）", len(mock.Requests))
	}
	if res.LocalGate.OK {
		t.Fatal("local gate should stay failing for the caller")
	}
	var local, ai int
	for _, f := range res.Candidate.Findings {
		switch f.Source {
		case "local_gate":
			local++
		case "ai_review":
			ai++
		}
	}
	if local != len(res.LocalGate.Findings) {
		t.Fatalf("local findings duplicated: candidate=%d gate=%d", local, len(res.LocalGate.Findings))
	}
	if ai != 1 {
		t.Fatalf("ai findings=%d want 1", ai)
	}
}

// 按章定向修复：只有硬门不过的章被重生成，通过的章不动。
func TestRepairFailedChaptersOnly(t *testing.T) {
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	snap, err := books.LoadBook(root, true)
	if err != nil {
		t.Fatal(err)
	}
	unit := snap.Units[0]
	long := ""
	for i := 0; i < 200; i++ {
		long += "他握紧刀柄，没有退缩。"
	}
	mock := llm.NewMock().
		AddText(`{"chapters":[{"chapter":1,"title":"一","body":"` + long + `"}]}`).
		AddText(`{"chapters":[{"chapter":2,"title":"二","body":"短。"}]}`). // 第2章字数不足，触发修复
		AddText(`{"chapters":[{"chapter":3,"title":"三","body":"` + long + `"}]}`).
		AddText(`{"chapters":[{"chapter":2,"title":"二·修","body":"` + long + `"}]}`) // 修复只针对第2章
	r := New(mock)
	res, err := r.RunWrite(context.Background(), snap, unit, "", "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// 3 章生成 + 1 次第2章修复 = 4 次调用
	if len(res.Calls) != 4 {
		t.Fatalf("calls=%d want 4 (3 generate + 1 chapter repair)", len(res.Calls))
	}
	if len(res.Candidate.Chapters) != 3 {
		t.Fatalf("chapters=%d", len(res.Candidate.Chapters))
	}
	// 第1、3章原样，第2章被替换为修复版
	if res.Candidate.Chapters[0].Title != "一" || res.Candidate.Chapters[2].Title != "三" {
		t.Fatal("passing chapters must be untouched")
	}
	if res.Candidate.Chapters[1].Title != "二·修" {
		t.Fatalf("chapter 2 should be repaired, got %q", res.Candidate.Chapters[1].Title)
	}
	// 修复调用 step 标记
	last := res.Calls[len(res.Calls)-1]
	if last.Step != "repair-ch2" {
		t.Fatalf("last call step=%q want repair-ch2", last.Step)
	}
}
