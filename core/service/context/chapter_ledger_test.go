package contextsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// 上一章账本只来自已定稿的 actual，不从审校候选或当前生成内容伪造。
func TestPreviousChapterLedgerFromAcceptedActual(t *testing.T) {
	root := t.TempDir()
	prev := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 1,
		ProsePath: "units/v01-U1.md", ProseRaw: "## 第1章 报到\n\n正文结尾。",
	}
	next := domain.UnitInfo{
		UnitID: "v01-U2", Volume: 1, ChapterFrom: 2, ChapterTo: 2,
	}
	actual := []domain.ChapterActual{{
		Chapter:  1,
		Summary:  "陆沉完成报到，接手旧案卷宗。",
		StoryDay: 1, EndDay: 2,
		StateDeltas: []domain.ChapterActualDelta{
			{Stem: "lu-chen", Field: "location", From: "家中", To: "双桥派出所",
				Evidence: "他到双桥派出所报到", Confirmed: true},
			// 未确认的迁移不得进入下一章的约束。
			{Stem: "lu-chen", Field: "resources", From: "无", To: "未知",
				Evidence: "", Confirmed: false},
		},
		Plant: []string{"FS-01"},
	}}
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(books.ActualPath("v01-U1"))), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := books.MarshalActual("v01-U1", actual)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, books.ActualPath("v01-U1")), body, 0o644); err != nil {
		t.Fatal(err)
	}
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo", RootPath: root},
		Units:   []domain.UnitInfo{prev, next},
	}

	got := PreviousChapterLedger(snap, next, 2)
	if got == "" {
		t.Fatal("上一章已定稿账本必须作为下一章输入")
	}
	for _, want := range []string{"陆沉完成报到", "双桥派出所", "FS-01", "已接受事实"} {
		if !strings.Contains(got, want) {
			t.Fatalf("账本桥接缺少 %q：\n%s", want, got)
		}
	}
	// 未确认的状态迁移不该被投影成事实。
	if strings.Contains(got, "resources") {
		t.Fatalf("未确认的迁移不应进入桥接：\n%s", got)
	}
}

// 单元内部衔接不能伪造账本：上一章还在本次 run 里刚生成，没经过接受。
func TestPreviousChapterLedgerSkipsInUnitChapters(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U1", Volume: 1, ChapterFrom: 7, ChapterTo: 10}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "demo"}, Units: []domain.UnitInfo{unit}}
	got := PreviousChapterLedger(snap, unit, 9)
	if got != "" {
		t.Fatalf("单元内部上一章未定稿，不能当成账本：\n%s", got)
	}
}

// 账本与章号对不上时不注入，避免把上一单元另一章的事实挪过来。
func TestPreviousChapterLedgerRejectsMismatchedChapter(t *testing.T) {
	root := t.TempDir()
	prev := domain.UnitInfo{UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 4, ProseRaw: "## 第1章\n\n正文"}
	next := domain.UnitInfo{UnitID: "v01-U2", Volume: 1, ChapterFrom: 5}
	actual := []domain.ChapterActual{{Chapter: 3, Summary: "不对位的章"}}
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(books.ActualPath("v01-U1"))), 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := books.MarshalActual("v01-U1", actual)
	if err := os.WriteFile(filepath.Join(root, books.ActualPath("v01-U1")), body, 0o644); err != nil {
		t.Fatal(err)
	}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "demo", RootPath: root}, Units: []domain.UnitInfo{prev, next}}
	if got := PreviousChapterLedger(snap, next, 5); got != "" {
		t.Fatalf("账本章号与上一章不一致时不该注入：\n%s", got)
	}
}
