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
// 现在必须报错，让作者改选带章号的意见或先按章审校。
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
	if !strings.Contains(err.Error(), "未标章号") {
		t.Fatalf("error should say why it cannot be located: %v", err)
	}
	// 一次调用都不该发：没有落点就没有可修的东西。
	if fake.count() != 0 {
		t.Fatalf("calls=%d want 0 (nothing to revise)", fake.count())
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
	if !strings.Contains(err.Error(), "无法定位") {
		t.Fatalf("error=%v", err)
	}
}

// 正常情况仍然要能修：带章号的意见不受上面两条影响。
func TestReviseStillWorksWithChapteredFinding(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	findings := []domain.ReviewFinding{
		{ID: "style-1-1", Severity: "blocking", Chapter: 1, Title: "对白说明化", Evidence: "e"},
	}
	fake := &repairProbeLLM{responses: []string{
		`{"chapter":1,"title":"第1章","body":"修好的正文。"}`,
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
