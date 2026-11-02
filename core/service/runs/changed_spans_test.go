package runs

import (
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 复审范围只包含改动句及前后各一句：改修在相邻句里时也要能看见。
func TestChangedSpansIncludesNeighbors(t *testing.T) {
	base := []domain.ChapterDraft{{
		Chapter: 6,
		Body:    "第一句。\n第二句。\n第三句。\n第四句。\n第五句。",
	}}
	revised := []domain.ChapterDraft{{
		Chapter: 6,
		Body:    "第一句。\n第二句改。\n第三句。\n第四句。\n第五句。",
	}}
	got := ChangedSpans(base, revised)
	spans, ok := got[6]
	if !ok {
		t.Fatalf("chapter 6 missing from changed spans: %+v", got)
	}
	want := map[string]bool{"第一句。": true, "第二句改。": true, "第三句。": true}
	if len(spans) != len(want) {
		t.Fatalf("spans=%v want 3 (changed + one neighbor each side)", spans)
	}
	for _, s := range spans {
		if !want[s] {
			t.Fatalf("unexpected span %q in %v", s, spans)
		}
	}
}

// 逐字未变的章不出现在结果里：它们没有任何「新问题」可言。
// 这是清单能收敛的关键——实测 96%+ 的复审"新问题"落在这些章上。
func TestChangedSpansSkipsUnchangedChapters(t *testing.T) {
	base := []domain.ChapterDraft{
		{Chapter: 6, Body: "第一句。"},
		{Chapter: 7, Body: "另一章。"},
	}
	revised := []domain.ChapterDraft{
		{Chapter: 6, Body: "第一句改。"},
		{Chapter: 7, Body: "另一章。"},
	}
	got := ChangedSpans(base, revised)
	if _, ok := got[7]; ok {
		t.Fatalf("unchanged chapter must not be scoped: %+v", got)
	}
	if _, ok := got[6]; !ok {
		t.Fatal("changed chapter must be scoped")
	}
}

// 只有空白/换行变化不算改动：否则一次纯排版会让整章重新进入复审范围。
func TestChangedSpansIgnoresWhitespaceOnlyChanges(t *testing.T) {
	base := []domain.ChapterDraft{{Chapter: 6, Body: "第一句。第二句。"}}
	revised := []domain.ChapterDraft{{Chapter: 6, Body: "第一句。 第二句。\n"}}
	if got := ChangedSpans(base, revised); len(got) != 0 {
		t.Fatalf("whitespace-only change must not scope the chapter: %+v", got)
	}
}

func TestChangedSpansHandlesMissingBase(t *testing.T) {
	if got := ChangedSpans(nil, []domain.ChapterDraft{{Chapter: 6, Body: "x"}}); got != nil {
		t.Fatalf("no base chapters means nothing was revised: %+v", got)
	}
}
