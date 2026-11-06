package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 逐章写作时，第 N 章的请求里必须带第 N-1 章的文末。
//
// 这条曾经被怀疑整条链路漏了：POST /runs/preview 对 write 只返回基础包，里面
// 既没有 chapterRule 也没有 previousChapterTail，看上去像"衔接数据没进 prompt"。
// 实际上真实调用是带上的（extra 在 RunWrite 里逐章拼）——但**预览与真实请求
// 不一致**本身就是缺陷，因为作者正是用预览判断模型看到了什么。
//
// 这条测试直接抓模型真实收到的 user prompt，把"到底带了没带"钉死。
func TestWritePacketCarriesPreviousChapterTail(t *testing.T) {
	unit := twoChapterUnit()
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo", Title: "样本"},
		Units:   []domain.UnitInfo{unit},
	}

	mock := llm.NewMock()
	mock.AddText(`{"chapters":[{"chapter":1,"title":"一","body":"` + strings.Repeat("第一章正文。", 400) + `"}]}`)
	mock.AddText(`{"chapters":[{"chapter":2,"title":"二","body":"` + strings.Repeat("第二章正文。", 400) + `"}]}`)

	runner := New(mock)
	res, err := runner.RunWrite(context.Background(), snap, unit, "", "test-model")
	if err != nil {
		t.Fatalf("RunWrite: %v", err)
	}
	if len(mock.Requests) != 2 {
		t.Fatalf("应记录 2 个请求，实际 %d（calls=%d）", len(mock.Requests), len(res.Calls))
	}

	first := mock.Requests[0].Messages[1].Content
	if strings.Contains(first, "previousChapterTail") {
		t.Fatal("第一章不该带上一章尾部")
	}
	second := mock.Requests[1].Messages[1].Content
	if !strings.Contains(second, "previousChapterTail") {
		t.Fatal("第二章的请求里没有 previousChapterTail：章间衔接数据漏了")
	}
	if !strings.Contains(second, "第一章正文") {
		t.Fatal("previousChapterTail 里不是上一章的正文内容")
	}
	// 另外两个按章变化的部分也必须进真实请求。
	for _, key := range []string{"chapterRule", `"chapter"`} {
		if !strings.Contains(second, key) {
			t.Fatalf("第二章请求缺少 %q", key)
		}
	}
}

func twoChapterUnit() domain.UnitInfo {
	return domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 2,
		OutlinePath: "outline/units/v01-U1.yaml",
		OutlineRaw: `unit_id: v01-U1
status: accepted
title_working: "两章"
desire: "甲"
obstacle: "乙"
choice: "丙"
payoff: "丁"
chapter_chars: 2500
on_stage: [lu]
pov: lu
state_deltas: []
chapters:
  - chapter: 1
    goal: "第一件事"
    change: "状态改变一"
    cut_hook: "钩子一"
    day_offset: 0
  - chapter: 2
    goal: "第二件事"
    change: "状态改变二"
    cut_hook: "钩子二"
    day_offset: 1
`,
		ChapterTarget: 2500,
	}
}
