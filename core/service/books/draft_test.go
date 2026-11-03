package books

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestCreateBookFromDraftWritesIdeaAndMinimalFiles(t *testing.T) {
	root, err := CreateBookFromDraft(t.TempDir(), DraftInput{
		BookID: "My Novel", Title: "雪夜", Genre: "悬疑", Subgenre: "刑侦探案",
		Platform: "番茄 · 男频", Idea: "新警撞见旧案卷宗", ChapterChars: "3000", Taboos: "不写真实案件",
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != "my-novel" {
		t.Fatalf("book id not slugified: %s", root)
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(snap.Files[domain.IdeaPath]) == "" {
		t.Fatal("outline/idea.md not written")
	}
	if !strings.Contains(snap.Files[domain.IdeaPath], "新警撞见旧案卷宗") {
		t.Fatal("idea input missing from outline/idea.md")
	}
	if !strings.Contains(snap.Files["book-bible.md"], "雪夜") {
		t.Fatal("book-bible.md not prefilled")
	}
	if !strings.Contains(snap.Files["canon/platform-positioning.md"], "番茄") {
		t.Fatal("platform-positioning.md not prefilled")
	}
	// 最小树：立项前不铺 9 个空设定模板，避免占位符被当成既有 canon。
	for _, path := range PlanSettingsPaths {
		if path == "book-bible.md" || path == "canon/platform-positioning.md" {
			continue
		}
		if strings.TrimSpace(snap.Files[path]) != "" {
			t.Fatalf("%s should not exist before 立项", path)
		}
	}
}

func TestCreateBookFromDraftRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateBookFromDraft(dir, DraftInput{BookID: "same", Title: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateBookFromDraft(dir, DraftInput{BookID: "same", Title: "B"}); err == nil {
		t.Fatal("duplicate book id should fail")
	}
}

func TestHasIdeaProposalIgnoresEmptyTemplate(t *testing.T) {
	if HasIdeaProposal(domain.BookSnapshot{Files: map[string]string{domain.IdeaPath: IdeaMarkdown(DraftInput{Title: "x"})}}) {
		t.Fatal("template placeholder must not count as a proposal")
	}
	raw := MergeIdeaProposal(IdeaMarkdown(DraftInput{Title: "x", Idea: "一句话"}), "## 推荐主线\n主角选择行动")
	if !HasIdeaProposal(domain.BookSnapshot{Files: map[string]string{domain.IdeaPath: raw}}) {
		t.Fatal("merged proposal should count")
	}
	if !strings.Contains(raw, "## 立项输入") || !strings.Contains(raw, "> 一句话") {
		t.Fatalf("merge must preserve author intake: %s", raw)
	}
}

func TestSelectPrimaryActionPrefersIdea(t *testing.T) {
	base := domain.BookSnapshot{Files: map[string]string{
		domain.IdeaPath: IdeaMarkdown(DraftInput{Title: "x", Idea: "一句话"}),
		"book-bible.md": "# bible\n",
	}}
	if kind, _, _ := SelectPrimaryAction(base); kind != domain.TaskIdea {
		t.Fatalf("want idea, got %s", kind)
	}
	base.Files[domain.IdeaPath] = MergeIdeaProposal(base.Files[domain.IdeaPath], "## 推荐主线\n完整提案")
	if kind, _, _ := SelectPrimaryAction(base); kind != domain.TaskPlan {
		t.Fatalf("want plan after proposal, got %s", kind)
	}
	// 旧书没有 idea.md：仍然走立项。
	old := domain.BookSnapshot{Files: map[string]string{"book-bible.md": "# bible\n"}}
	if kind, _, _ := SelectPrimaryAction(old); kind != domain.TaskPlan {
		t.Fatalf("legacy book want plan, got %s", kind)
	}
}

func TestMergeIdeaProposalKeepsIntakeWhenNoSection(t *testing.T) {
	raw := "# 立项 · x\n\n## 立项输入\n- 题材：悬疑\n"
	merged := MergeIdeaProposal(raw, "## 推荐主线\n内容")
	if !strings.Contains(merged, "- 题材：悬疑") || !strings.Contains(merged, IdeaProposalSection) {
		t.Fatalf("unexpected merge: %s", merged)
	}
}
