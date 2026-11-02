package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 补丁锚点错一次 → 第二轮带着具体原因重试，system prompt 一字不改。
// system 不变是前缀缓存的前提：改了它就等于每次重试都在按全价重算本章正文。
func TestReviseRepairsFailedAnchorThenSucceeds(t *testing.T) {
	snap, unit := reviseFixture(t)
	fake := &repairProbeLLM{responses: []string{
		// 第一轮：锚点抄错了一个字，本章找不到。
		`{"chapter":2,"edits":[{"find":"石门缓缓打开，他往前迈了一步，没有回头看。","replace":"石门缓缓打开，他往前迈了一步，又退了回来。"}]}`,
		// 第二轮：按回喂的"最接近的原文"改对。
		ch2Patch,
	}}
	r := New(fake)

	res, err := r.RunRevise(context.Background(), snap, unit,
		[]domain.ReviewFinding{{ID: "f2", Severity: "blocking", Chapter: 2, Title: "位置跳变"}},
		"mock/mock-gpt-4")
	if err != nil {
		t.Fatalf("the repair round should succeed: %v", err)
	}
	if fake.count() != 2 {
		t.Fatalf("calls=%d want 2 (patch + repair)", fake.count())
	}
	if fake.systems[0] != fake.systems[1] {
		t.Fatal("system prompt changed between attempts; prefix cache would miss")
	}
	// 第二轮必须带着可执行的定位原因，而不是一句"格式不对"。
	repair := fake.repairPayload(1)
	if !strings.Contains(repair, "找不到") && !strings.Contains(repair, "最接近的原文") {
		t.Fatalf("repair feedback should carry the anchor failure: %s", repair)
	}
	if !strings.Contains(res.Candidate.Chapters[1].Body, "又退了回来") {
		t.Fatalf("chapter 2 not revised: %q", res.Candidate.Chapters[1].Body)
	}
	// 调用记录要能分辨补丁轮与兜底轮，否则成本无从归属。
	if len(res.Calls) != 2 || res.Calls[0].Step != "revise-ch2" || res.Calls[1].Step != "revise-ch2-repair" {
		t.Fatalf("call steps=%v", stepsOf(res.Calls))
	}
}

// 两次补丁都套不上 → 第三次退回整章正文，仍然要产出可落盘的候选。
// 兜底不是"更聪明的补丁"，而是唯一必然能成功的输出契约。
func TestReviseFallsBackToWholeChapterAfterTwoPatchFailures(t *testing.T) {
	snap, unit := reviseFixture(t)
	fake := &repairProbeLLM{responses: []string{
		`{"chapter":2,"edits":[{"find":"这一段正文里根本没有。","replace":"x"}]}`,
		`{"chapter":2,"edits":[{"find":"……","replace":"y"}]}`,
		ch2Fallback,
	}}
	r := New(fake)

	res, err := r.RunRevise(context.Background(), snap, unit,
		[]domain.ReviewFinding{{ID: "f2", Severity: "blocking", Chapter: 2, Title: "位置跳变"}},
		"mock/mock-gpt-4")
	if err != nil {
		t.Fatalf("the whole-chapter fallback should succeed: %v", err)
	}
	if fake.count() != 3 {
		t.Fatalf("calls=%d want 3 (patch + repair + fallback)", fake.count())
	}
	if got := stepsOf(res.Calls); len(got) != 3 || got[2] != "revise-ch2-fallback" {
		t.Fatalf("call steps=%v", got)
	}
	// 兜底轮换的是输出契约，前两轮仍是同一个 system prompt。
	if fake.systems[0] != fake.systems[1] {
		t.Fatal("patch and repair must share one system prompt")
	}
	if fake.systems[2] == fake.systems[1] {
		t.Fatal("the fallback must switch to the whole-chapter contract")
	}
	if res.Candidate.Chapters[1].Body != "第2章修订后的正文" {
		t.Fatalf("chapter 2 body=%q", res.Candidate.Chapters[1].Body)
	}
}

// 三次都失败必须报错，且调用数不超过申报的预算。
func TestReviseStopsAfterBudget(t *testing.T) {
	snap, unit := reviseFixture(t)
	fake := &repairProbeLLM{responses: []string{
		`{"chapter":2,"edits":[{"find":"没有这句。","replace":"x"}]}`,
		`{"chapter":2,"edits":[{"find":"也没有这句。","replace":"y"}]}`,
		`{"chapter":2,"edits":[{"find":"仍然没有。","replace":"z"}]}`,
	}}
	r := New(fake)

	res, err := r.RunRevise(context.Background(), snap, unit,
		[]domain.ReviewFinding{{ID: "f2", Severity: "blocking", Chapter: 2, Title: "位置跳变"}},
		"mock/mock-gpt-4")
	if err == nil {
		t.Fatal("three failed attempts must surface an error")
	}
	if len(res.Calls) != 3 {
		t.Fatalf("calls=%d want 3 (the declared budget)", len(res.Calls))
	}
	if res.Packet.Policy.MaxCalls != 3 {
		t.Fatalf("maxCalls=%d want 3", res.Packet.Policy.MaxCalls)
	}
	// 报错要能把三次失败都说到，只报最后一次会把真正的原因埋掉。
	if !strings.Contains(err.Error(), "revise-ch2") {
		t.Fatalf("error should name the failing steps: %v", err)
	}
}

func stepsOf(calls []domain.CallRecord) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Step)
	}
	return out
}
