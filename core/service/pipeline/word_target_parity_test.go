package pipeline

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// 写作与审校必须用同一个每章字数目标。
//
// 旧代码写死 2000（硬门阈值 = 0.6×2000 = 1200 字），审校却用
// books.ChapterTarget（细纲字数目标）。两个目标一旦分叉，同一章会在写作阶段
// 通过、在审校阶段立刻判 blocking，自动修订再修一轮，整条流程必然停住。
func TestWriteGateUsesSameChapterTargetAsReview(t *testing.T) {
	// 造一个每章目标 3500 的单元（单章），文字落在两个阈值之间：
	// 写死 2000 时阈值 1800 判定通过，按 3500 判定阈值 2100，判不足。
	unit := domain.UnitInfo{
		UnitID: "v01-U1", ChapterFrom: 1, ChapterTo: 1,
		OutlineRaw: "word_target: 3500\n",
	}
	target := books.ChapterTarget(unit)
	if target != 3500 {
		t.Fatalf("target=%d want 3500", target)
	}

	chars := 1900
	body := strings.Repeat("雨", chars)
	chapters := []domain.ChapterDraft{{Chapter: 1, Body: body}}

	if rep := gates.CheckProseWithRules(chapters, 2000, nil); !rep.OK {
		t.Fatalf("with a hardcoded 2000 target this chapter should have passed the write gate (that was the bug)")
	}
	if rep := gates.CheckProseWithRules(chapters, target, nil); rep.OK {
		t.Fatalf("%d chars against target %d must block", chars, target)
	}
}

// 端到端：写作阶段就该发现字数不足并触发按章修复，而不是留给审校。
func TestRunWriteRepairsShortChapterBeforeReview(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ChapterFrom, unit.ChapterTo = 1, 1
	target := books.ChapterTarget(unit)

	fake := &shortThenLongLLM{shortChars: target / 3, longChars: target}
	r := New(fake)

	res, err := r.RunWrite(context.Background(), snap, unit, "", "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	// 第一次写过短 → 本地硬门不通过 → 按章修复重生成。
	if got := fake.count(); got < 2 {
		t.Fatalf("calls=%d want >=2 (generate + gate repair)", got)
	}
	if !res.LocalGate.OK {
		t.Fatalf("after repair the gate should pass, findings=%+v", res.LocalGate.Findings)
	}
	var hasRepair bool
	for _, c := range res.Calls {
		if strings.Contains(c.Step, "repair") {
			hasRepair = true
		}
	}
	if !hasRepair {
		t.Fatalf("repair call not labeled: %+v", res.Calls)
	}
}

// shortThenLongLLM 第一次返回过短正文，之后返回达标正文。
type shortThenLongLLM struct {
	mu         sync.Mutex
	n          int
	shortChars int
	longChars  int
}

func (s *shortThenLongLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	body := strings.Repeat("雨落在青石上，他握紧了刀柄。", s.longChars/16+1)[:0]
	if s.n == 1 {
		body = strings.Repeat("雨", s.shortChars)
	} else {
		body = strings.Repeat("雨落在青石上。", s.longChars/7+1)
	}
	out := `{"chapters":[{"chapter":1,"title":"第1章","body":"` + body + `"}]}`
	return port.LLMChatResponse{Content: out, Done: true}, nil
}

func (s *shortThenLongLLM) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}
