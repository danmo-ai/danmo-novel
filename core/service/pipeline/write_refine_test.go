package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// 优化模式：请求里必须真的带上旧稿，候选必须能 diff，改动幅度必须有回执。
func TestWriteRefineCarriesDraftAndBaseline(t *testing.T) {
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	snap, err := books.LoadBook(root, true)
	if err != nil {
		t.Fatal(err)
	}
	body := ""
	for i := 0; i < 60; i++ {
		body += "林尘握住刀柄，指节因为用力而微微发白，他没有说话，只是看着试炼场尽头那道缓缓打开的石门。"
	}
	unit := snap.Units[0]
	unit.ChapterFrom, unit.ChapterTo = 1, 1
	unit.ProseRaw = "## 第1章 一\n\n" + body + "\n"
	unit.HasProse = true

	mock := llm.NewMock().AddText(`{"chapters":[{"chapter":1,"title":"一","body":"` + body + `"}]}`)
	r := New(mock)
	res, err := r.RunWriteWithMode(context.Background(), snap, unit, "", domain.RewriteRefine, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidate.BaseChapters) != 1 {
		t.Fatalf("baseChapters=%d want 1 (candidate must be diffable)", len(res.Candidate.BaseChapters))
	}
	sent := mock.Requests[0].Messages[1].Content
	if !strings.Contains(sent, "original-prose") {
		t.Fatal("refine request must carry the old draft")
	}
	found := false
	for _, f := range res.Candidate.Findings {
		if f.Lens == "refine" && f.Severity == "advisory" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unchanged refine output should raise an advisory: %+v", res.Candidate.Findings)
	}
}

// fresh 模式不得为了"顺手"把旧稿塞进请求：那会改变生成意图，也改变成本。
func TestWriteFreshIgnoresExistingDraft(t *testing.T) {
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	snap, err := books.LoadBook(root, true)
	if err != nil {
		t.Fatal(err)
	}
	body := ""
	for i := 0; i < 60; i++ {
		body += "林尘握住刀柄，指节因为用力而微微发白，他没有说话，只是看着试炼场尽头那道缓缓打开的石门。"
	}
	unit := snap.Units[0]
	unit.ChapterFrom, unit.ChapterTo = 1, 1
	unit.ProseRaw = "## 第1章 一\n\n" + body + "\n"
	unit.HasProse = true

	mock := llm.NewMock().AddText(`{"chapters":[{"chapter":1,"title":"二","body":"` + body + `"}]}`)
	r := New(mock)
	res, err := r.RunWriteWithMode(context.Background(), snap, unit, "", domain.RewriteFresh, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	sent := mock.Requests[0].Messages[1].Content
	if strings.Contains(sent, "original-prose") {
		t.Fatal("fresh write must not carry the old draft")
	}
	if len(res.Candidate.BaseChapters) != 0 {
		t.Fatalf("fresh candidate must have no baseline, got %d", len(res.Candidate.BaseChapters))
	}
}
