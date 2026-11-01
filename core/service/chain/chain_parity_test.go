package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

// 审校是否分章必须读同一份设置：人工路径在 runs.Service 里读
// settings.review.splitByChapter，链若强行 true，同一个单元在两条路下会得到
// 不同粒度、不同条数的 findings。
func TestChainReviewFollowsSplitSetting(t *testing.T) {
	for _, split := range []bool{true, false} {
		name := "unit-wide"
		if split {
			name = "split"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if err := h.svc.Settings.Update(func(s *config.Settings) {
				s.Review.SplitByChapter = split
			}); err != nil {
				t.Fatal(err)
			}
			ch := h.start(t, domain.ChainSpec{})
			if ch.Status != domain.ChainCompleted {
				t.Fatalf("status=%s err=%s", ch.Status, ch.Error)
			}
			// 夹具单元有 3 章：分章 = 3 次审校调用，整单元 = 1 次。
			want := 1
			if split {
				want = 3
			}
			if got := h.countTask(domain.TaskReview); got != want {
				t.Fatalf("review calls=%d want %d (setting split=%v)", got, want, split)
			}
		})
	}
}

// 人工审校的确认框里能勾参考知识文章；链必须能透传，否则作者以为生效的规则
// 在自动流程里根本没注入。
func TestChainPassesReviewKnowledge(t *testing.T) {
	h := newHarness(t)
	writeKnowledgeDoc(t, h, "review-style")

	ch := h.start(t, domain.ChainSpec{
		Review: domain.ChainStageConfig{KnowledgeIDs: []string{"review-style"}},
	})
	if ch.Status != domain.ChainCompleted {
		t.Fatalf("status=%s err=%s", ch.Status, ch.Error)
	}

	var sawReviewRef bool
	for _, req := range h.llm.captured() {
		payload, err := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
		if err != nil || payload.Task != string(domain.TaskReview) {
			continue
		}
		for _, ref := range payload.References {
			if strings.Contains(ref.Content, "review-style-marker") {
				sawReviewRef = true
			}
		}
	}
	if !sawReviewRef {
		t.Fatal("review knowledge never reached the review prompt")
	}
}

// 审校阶段没有"追加指令"的语义：规则来自审校清单与本地 gate。作者传进来也
// 必须被丢掉，否则自由文本会和清单冲突。
func TestChainDropsReviewInstruction(t *testing.T) {
	spec := normalizeSpec(domain.ChainSpec{
		Review: domain.ChainStageConfig{Instruction: "随便改改就行"},
	})
	if spec.Review.Instruction != "" {
		t.Fatalf("review instruction should be dropped, got %q", spec.Review.Instruction)
	}
}

// writeKnowledgeDoc drops one article into the configured library.
func writeKnowledgeDoc(t *testing.T, h *harness, id string) {
	t.Helper()
	root := h.svc.Settings.Get().LibraryRoot
	if root == "" {
		t.Fatal("library root not configured")
	}
	path := filepath.Join(root, "knowledge", id+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nstages: [review]\ntitle: 审校参考\n---\n\nreview-style-marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
