package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// reviseFixture builds a unit whose chapters carry unique anchors.
//
// 通用 fixture 用同一句话重复上百次拼出正文，那样的正文在「章内唯一」契约下
// 没有任何可用锚点（每一句都出现上百次），测不出补丁路径。这里给每章一句
// 只出现一次的记忆点，其余仍是重复文本——更接近真实网文。
func reviseFixture(t *testing.T) (domain.BookSnapshot, domain.UnitInfo) {
	t.Helper()
	snap, unit, _ := loadFixtureUnit(t)
	unit.ProseRaw = "## 第1章 夜\n\n" +
		strings.Repeat("他握紧刀柄，没有退缩，只把呼吸压到最慢。", 30) +
		"\n\n他在廊下站了很久。\n\n" +
		"## 第2章 门\n\n石门缓缓打开，他往前迈了一步，没有回头。\n\n" +
		strings.Repeat("风从门缝里灌进来，吹得火把直晃。", 30) +
		"\n\n他把手按在门框上。\n\n" +
		"## 第3章 影\n\n" +
		strings.Repeat("影子在墙上拉长，他听见身后有人在呼吸。", 30) +
		"\n\n他停在台阶上，没有再往下走。"
	return snap, unit
}

const (
	// 第 2 章里只出现一次的一句，作为补丁锚点。
	ch2Anchor   = "石门缓缓打开，他往前迈了一步，没有回头。"
	ch2Patch    = `{"chapter":2,"edits":[{"find":"` + ch2Anchor + `","replace":"石门缓缓打开，他往前迈了一步，又退了回来。"}]}`
	ch2Fallback = `{"chapter":2,"title":"门·修","body":"第2章修订后的正文"}`
)

// 修订只改有 findings 的章：未涉及的章必须逐字保留，否则"最小改动"无法兑现。
func TestRunReviseOnlyTouchesTargetedChapters(t *testing.T) {
	snap, unit := reviseFixture(t)
	mock := llm.NewMock().AddText(ch2Patch)
	r := New(mock)

	findings := []domain.ReviewFinding{
		{ID: "f2", Severity: "blocking", Chapter: 2, Title: "位置跳变"},
	}
	res, err := r.RunRevise(context.Background(), snap, unit, findings, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// 修订前的正文必须留在候选里，否则前端无法 diff，接受就是盲签。
	if len(res.Candidate.BaseChapters) != len(res.Candidate.Chapters) {
		t.Fatalf("baseChapters=%d chapters=%d, want same length for diff",
			len(res.Candidate.BaseChapters), len(res.Candidate.Chapters))
	}
	origChapters := gates.ParseProseChapters(unit.ProseRaw)
	for i, ch := range res.Candidate.BaseChapters {
		if ch.Body != origChapters[i].Body {
			t.Fatalf("base chapter %d is not the pre-revise prose", ch.Chapter)
		}
	}
	// 只有第 2 章需要改 → 只发一次调用：补丁一次套上，不该再走修复或兜底。
	if len(mock.Requests) != 1 {
		t.Fatalf("requests=%d want 1 (patch applied on the first try)", len(mock.Requests))
	}
	if len(res.Candidate.Chapters) != 3 {
		t.Fatalf("chapters=%d want all 3 kept", len(res.Candidate.Chapters))
	}
	if !strings.Contains(res.Candidate.Chapters[1].Body, "又退了回来") {
		t.Fatalf("chapter 2 not revised: %q", res.Candidate.Chapters[1].Body)
	}
	// 逐字比对：第 1、3 章必须与原文完全一致。
	for _, i := range []int{0, 2} {
		if res.Candidate.Chapters[i].Body != origChapters[i].Body {
			t.Fatalf("chapter %d modified although out of scope", i+1)
		}
	}
	// 预算按"实际章的章数 × 每章 3 次"申报，UI 才不会显示越界数字。
	if res.Packet.Policy.MaxCalls != 3 {
		t.Fatalf("maxCalls=%d want 3 (one touched chapter × 3 attempts)", res.Packet.Policy.MaxCalls)
	}
}

// 模型返回错误章号时必须拒绝，不能把别的章的正文写回目标章。
func TestRunReviseRejectsWrongChapter(t *testing.T) {
	snap, unit := reviseFixture(t)
	mock := llm.NewMock().
		AddText(`{"chapter":9,"edits":[{"find":"` + ch2Anchor + `","replace":"不该被接受"}]}`)
	r := New(mock)

	_, err := r.RunRevise(context.Background(), snap, unit,
		[]domain.ReviewFinding{{ID: "f2", Severity: "blocking", Chapter: 2, Title: "x"}},
		"mock/mock-gpt-4")
	if err == nil {
		t.Fatal("wrong chapter number must be rejected")
	}
	// 报错必须保留"章号不对"这条具体原因：只看最后一次失败会把真正的原因埋掉。
	if !strings.Contains(err.Error(), "chapter") {
		t.Fatalf("error should mention chapter: %v", err)
	}
	if !strings.Contains(err.Error(), "9") {
		t.Fatalf("error should carry the chapter the model actually returned: %v", err)
	}
}

func splitFixtureChapters(t *testing.T, raw string) []domain.ChapterDraft {
	t.Helper()
	return gates.ParseProseChapters(raw)
}

var _ = splitFixtureChapters
