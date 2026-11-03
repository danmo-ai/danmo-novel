package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

// sequencedLLM 按调用次序返回脚本化的回答，用来覆盖「第一次太短 → 修复」。
type sequencedLLM struct {
	replies []string
	reqs    []port.LLMChatRequest
}

func (s *sequencedLLM) Chat(ctx context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	s.reqs = append(s.reqs, req)
	i := len(s.reqs) - 1
	if i >= len(s.replies) {
		i = len(s.replies) - 1
	}
	return port.LLMChatResponse{Content: s.replies[i], Done: true}, nil
}

func ideaSnapshot() domain.BookSnapshot {
	return domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "x", Title: "雪夜", Genre: "悬疑", Subgenre: "刑侦探案"},
		Files: map[string]string{
			domain.IdeaPath:                 books.IdeaMarkdown(books.DraftInput{Title: "雪夜", Genre: "悬疑", Idea: "新警撞见旧案卷宗", Taboos: "不写真实案件"}),
			"book-bible.md":                 "# bible\n",
			"canon/platform-positioning.md": "# platform\n",
		},
	}
}

func TestRunIdeaExpandMergesProposalAndPreservesIntake(t *testing.T) {
	proposal := strings.Repeat("## 推荐主线\n主角在卷宗里发现自己的名字。\n", 6)
	llm := &sequencedLLM{replies: []string{proposal}}
	r := New(llm)
	res, err := r.RunIdeaExpand(context.Background(), ideaSnapshot(), "更冷硬", "mock/m")
	if err != nil {
		t.Fatal(err)
	}
	cand := res.Candidate
	if cand.Kind != domain.TaskIdea || cand.TargetPath != domain.IdeaPath {
		t.Fatalf("candidate=%+v", cand)
	}
	if !strings.Contains(cand.Replacement, "## 立项输入") || !strings.Contains(cand.Replacement, "新警撞见旧案卷宗") {
		t.Fatal("merge must keep author intake intact")
	}
	if !strings.Contains(cand.Replacement, "主角在卷宗里发现自己的名字") {
		t.Fatal("proposal missing from merged doc")
	}
	if len(res.Calls) != 1 || res.Calls[0].Step != "generate" {
		t.Fatalf("calls=%+v", res.Calls)
	}
	// 自由输出：系统提示词不应要求 JSON 协议。
	if strings.Contains(llm.reqs[0].Messages[0].Content, "输出协议") {
		t.Fatal("idea stage must not carry a structured output protocol")
	}
}

func TestRunIdeaExpandRetriesShortOutput(t *testing.T) {
	short := "太短了"
	good := strings.Repeat("## 主角\n欲望、伤口与底线。\n", 10)
	llm := &sequencedLLM{replies: []string{short, good}}
	r := New(llm)
	res, err := r.RunIdeaExpand(context.Background(), ideaSnapshot(), "", "mock/m")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Calls) != 2 || res.Calls[1].Step != "repair" {
		t.Fatalf("want one repair call, got %+v", res.Calls)
	}
	if !strings.Contains(res.Candidate.Replacement, "欲望、伤口与底线") {
		t.Fatal("repaired proposal missing")
	}
	// repair 追加在 payload 末尾，保证前缀缓存仍然命中。
	user := llm.reqs[1].Messages[1].Content
	if !strings.Contains(user, "\"repair\"") {
		t.Fatal("repair payload not appended")
	}
}

func TestBuildIdeaPacketIncludesIntakeOnly(t *testing.T) {
	snap := ideaSnapshot()
	snap.Files["outline/book_outline.md"] = "# 总纲\n- schema_version: 1\n"
	p := contextsvc.BuildIdeaPacket(snap, "")
	var sources []string
	for _, b := range p.Blocks {
		sources = append(sources, b.Source)
	}
	joined := strings.Join(sources, ",")
	if !strings.Contains(joined, domain.IdeaPath) {
		t.Fatalf("idea block missing: %s", joined)
	}
	if strings.Contains(joined, "outline/book_outline.md") {
		t.Fatal("idea expansion must not be locked by the existing outline")
	}
	if p.TaskKind != domain.TaskIdea {
		t.Fatalf("task kind=%s", p.TaskKind)
	}
}

func TestBuildPlanPacketInitUsesIdeaAndSkipsScaffold(t *testing.T) {
	snap := ideaSnapshot()
	snap.Files[domain.IdeaPath] = books.MergeIdeaProposal(snap.Files[domain.IdeaPath], "## 推荐主线\n完整提案")
	// 旧路径留下的空脚手架：init 不能再把它们当既有 canon。
	snap.Files["canon/world.md"] = "# World\n\n## 力量 / 规则\n- 谁能做什么：\n"
	snap.Files["canon/locked-terms.yaml"] = "locked_until: {}\ncompliance: []\naliases: {}\n"
	snap.Files["outline/book_outline.md"] = ""

	p := contextsvc.BuildPlanPacketWithMode(snap, 1, domain.PlanModeInit, "")
	var sources []string
	for _, b := range p.Blocks {
		sources = append(sources, b.Source)
	}
	joined := strings.Join(sources, ",")
	if !strings.Contains(joined, domain.IdeaPath) {
		t.Fatalf("init must receive the confirmed idea: %s", joined)
	}
	if strings.Contains(joined, "canon/world.md") || strings.Contains(joined, "canon/locked-terms.yaml") {
		t.Fatalf("init must not inject scaffold placeholders: %s", joined)
	}

	// reinit 仍要看到已接受的世界观与总纲。
	snap.Files["outline/book_outline.md"] = "# 全书总纲\n- schema_version: 1\n"
	p2 := contextsvc.BuildPlanPacketWithMode(snap, 1, domain.PlanModeReinit, "")
	var sources2 []string
	for _, b := range p2.Blocks {
		sources2 = append(sources2, b.Source)
	}
	joined2 := strings.Join(sources2, ",")
	if !strings.Contains(joined2, "canon/world.md") || !strings.Contains(joined2, "outline/book_outline.md") {
		t.Fatalf("reinit should keep accepted canon: %s", joined2)
	}
}
