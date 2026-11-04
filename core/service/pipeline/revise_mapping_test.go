package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// 整单元审校产出的意见可以没有章号（"全单元节奏拖沓"）。逐章修订找不到落点，
// 曾经静默无操作——候选与原稿逐字节相同，作者点「确认落盘」等于什么都没发生。
// 现在必须报错，并且说清这类意见该去哪儿修，而不是让作者去猜"带章号"是什么意思。
func TestReviseRejectsFindingsWithoutChapter(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	findings := []domain.ReviewFinding{
		{ID: "unit-1", Severity: "blocking", Chapter: 0, Title: "全单元节奏拖沓", Evidence: "每章都在原地打转"},
	}
	fake := &repairProbeLLM{}
	r := New(fake)

	_, err := r.RunRevise(context.Background(), snap, unit, findings, "mock/mock-gpt-4")
	if err == nil {
		t.Fatal("want a loud error, got nil (silent no-op)")
	}
	if !strings.Contains(err.Error(), "全单元节奏拖沓") {
		t.Fatalf("error should name the offending finding: %v", err)
	}
	if !strings.Contains(err.Error(), "不属于任何一章正文") {
		t.Fatalf("error should say why it cannot be located: %v", err)
	}
	// 一次调用都不该发：没有落点就没有可修的东西。
	if fake.count() != 0 {
		t.Fatalf("calls=%d want 0 (nothing to revise)", fake.count())
	}
}

// 状态账本的陈旧字段同样没有章号，但它的修法很具体：细纲 state_deltas 写终值。
// 只说"改选带章号的意见"会把作者引向一个他根本找不到的东西。
func TestReviseExplainsWhereStateFindingsAreFixed(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	findings := []domain.ReviewFinding{{
		ID: "stale-lin-health", Severity: "advisory", Source: "local_gate", Lens: "continuity",
		Title: "人物状态可能已过期",
	}}
	fake := &repairProbeLLM{}
	r := New(fake)

	_, err := r.RunRevise(context.Background(), snap, unit, findings, "mock/mock-gpt-4")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"人物状态可能已过期", "state_deltas", "不属于任何一章正文"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
	if fake.count() != 0 {
		t.Fatalf("calls=%d want 0", fake.count())
	}
}

// 章号超出单元范围（例如引用了别的单元的章）同样不能静默跳过。
func TestReviseRejectsOutOfRangeChapter(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	last := chapters[len(chapters)-1].Chapter

	findings := []domain.ReviewFinding{
		{ID: "stale-1", Severity: "advisory", Chapter: last + 5, Title: "对白说明化", Evidence: "e"},
	}
	r := New(&repairProbeLLM{})

	_, err := r.RunRevise(context.Background(), snap, unit, findings, "mock/mock-gpt-4")
	if err == nil {
		t.Fatal("want an error for an out-of-range chapter")
	}
	if !strings.Contains(err.Error(), "章号") || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("error=%v", err)
	}
}

// 正常情况仍然要能修：带章号的意见不受上面两条影响。
func TestReviseStillWorksWithChapteredFinding(t *testing.T) {
	snap, unit := reviseFixture(t)
	findings := []domain.ReviewFinding{
		{ID: "style-1-1", Severity: "blocking", Chapter: 1, Title: "对白说明化", Evidence: "e"},
	}
	fake := &repairProbeLLM{responses: []string{
		`{"chapter":1,"edits":[{"find":"他在廊下站了很久。","replace":"他在廊下站了很久，直到更漏响过三遍。"}]}`,
	}}
	r := New(fake)

	if _, err := r.RunRevise(context.Background(), snap, unit, findings, "mock/mock-gpt-4"); err != nil {
		t.Fatal(err)
	}
	// 只修第 1 章：其他章不发起调用。
	if fake.count() != 1 {
		t.Fatalf("calls=%d want 1 (only the targeted chapter)", fake.count())
	}
}
