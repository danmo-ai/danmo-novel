package pipeline

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/port"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

// chapterScriptLLM 按章回放脚本，记录每次请求是否带 repair。
type chapterScriptLLM struct {
	mu    sync.Mutex
	reps  []bool
	calls int
	// badFirst 为真时，每章第一次返回空 body（结构不合规）。
	badFirst bool
}

func (c *chapterScriptLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, _ := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
	c.reps = append(c.reps, p.Repair != nil)
	ch := 0
	if v, ok := p.Extra["chapter"].(float64); ok {
		ch = int(v)
	}
	c.calls++
	// 每章第一次请求（无 repair）按脚本返回坏结构，带 repair 的必须修好。
	body := "雨落在青石上，他握紧了刀柄。" + strings.Repeat("他继续向前。", 400)
	if c.badFirst && p.Repair == nil {
		body = "   "
	}
	out := fmt.Sprintf(`{"chapters":[{"chapter":%d,"title":"第%d章","body":%q}]}`, ch, ch, body)
	return port.LLMChatResponse{Content: out, Done: true}, nil
}

func (c *chapterScriptLLM) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reps)
}

// 分章写作曾完全没有重试层：单章结构不合规（空 body）直接中断整单元，
// 前面已花掉的调用全部作废，候选也不生成。现在每章允许一次定向修复。
func TestSplitWriteRepairsStructuralFailurePerChapter(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ChapterFrom, unit.ChapterTo = 1, 2
	fake := &chapterScriptLLM{badFirst: true}
	r := New(fake)

	res, err := r.RunWrite(context.Background(), snap, unit, "", "mock/mock-gpt-4")
	if err != nil {
		t.Fatalf("structural failure should be repaired, got %v", err)
	}
	if len(res.Candidate.Chapters) != 2 {
		t.Fatalf("chapters=%d want 2", len(res.Candidate.Chapters))
	}
	for i, ch := range res.Candidate.Chapters {
		if strings.TrimSpace(ch.Body) == "" {
			t.Fatalf("chapter %d has empty body", i+1)
		}
	}

	// 第 1、2 次是每章的生成（无 repair），第 3、4 次是各自的结构修复。
	var repaired int
	for _, rep := range fake.reps {
		if rep {
			repaired++
		}
	}
	if repaired != 2 {
		t.Fatalf("repair calls=%d want 2 (one per chapter)", repaired)
	}
	if got := fake.count(); got < 4 {
		t.Fatalf("calls=%d want >= 4 (2 generate + 2 repair)", got)
	}
	// 修复调用的 step 要能和生成调用区分开，否则运行详情看不出哪次是重试。
	var sawChapterRepair bool
	for _, rec := range res.Calls {
		if strings.Contains(rec.Step, "-repair") {
			sawChapterRepair = true
		}
	}
	if !sawChapterRepair {
		t.Fatal("structural repair calls are not labeled")
	}
}

// 预算是真上限：每章最坏 3 次（生成 + 结构修复 + 硬门修复），不能再出现 5/4。
func TestSplitWriteBudgetCoversAllRepairLayers(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ChapterFrom, unit.ChapterTo = 1, 3
	fake := &chapterScriptLLM{badFirst: true}
	r := New(fake)

	res, err := r.RunWrite(context.Background(), snap, unit, "", "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if res.Packet.Policy.MaxCalls != 9 {
		t.Fatalf("maxCalls=%d want 9 (3 chapters x 3 layers)", res.Packet.Policy.MaxCalls)
	}
	if got := len(res.Calls); got > res.Packet.Policy.MaxCalls {
		t.Fatalf("calls=%d exceeds declared budget=%d", got, res.Packet.Policy.MaxCalls)
	}
}

// 仍然只修一次：带 repair 的第二次再失败，就该中断而不是死循环。
func TestSplitWriteRepairsOnlyOnce(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ChapterFrom, unit.ChapterTo = 1, 1
	fake := &alwaysEmptyLLM{}
	r := New(fake)

	_, err := r.RunWrite(context.Background(), snap, unit, "", "mock/mock-gpt-4")
	if err == nil {
		t.Fatal("want error after the single repair also fails")
	}
	if !strings.Contains(err.Error(), "body") {
		t.Fatalf("error should point at the field: %v", err)
	}
	if got := fake.count(); got != 2 {
		t.Fatalf("calls=%d want 2 (generate + exactly one repair)", got)
	}
}

type alwaysEmptyLLM struct {
	mu sync.Mutex
	n  int
}

func (a *alwaysEmptyLLM) Chat(_ context.Context, _ port.LLMChatRequest) (port.LLMChatResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return port.LLMChatResponse{Content: `{"chapters":[{"chapter":1,"title":"t","body":""}]}`, Done: true}, nil
}

func (a *alwaysEmptyLLM) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}
