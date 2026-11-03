package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/port"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

// cacheProbeLLM 记录每次请求的完整 prompt，并回放合格正文。
//
// firstPassShort 为真时，每章第一次（没有 repairChapter / repair 块）返回过短正文，
// 用来测「生成 → 按章硬门修复」这一层的缓存复用。
type cacheProbeLLM struct {
	mu             sync.Mutex
	systems        []string
	users          []string
	firstPassShort bool
}

func (c *cacheProbeLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	system := req.Messages[0].Content
	user := req.Messages[len(req.Messages)-1].Content
	p, _ := contextsvc.DecodeUserPayload(user)
	ch := 0
	if v, ok := p.Extra["chapter"].(float64); ok {
		ch = int(v)
	}
	c.mu.Lock()
	c.systems = append(c.systems, system)
	c.users = append(c.users, user)
	c.mu.Unlock()

	repairing := p.Repair != nil || p.Extra["repairChapter"] != nil
	body := strings.Repeat("雨落在青石上，他握紧了刀柄，没有回头。", 130)
	if c.firstPassShort && !repairing {
		body = "雨"
	}
	out, _ := json.Marshal(map[string]any{"chapters": []map[string]any{{
		"chapter": ch, "title": fmt.Sprintf("第%d章", ch), "body": body,
	}}})
	return port.LLMChatResponse{Content: string(out), Done: true}, nil
}

func (c *cacheProbeLLM) prompts(i int) (system, user string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.systems[i], c.users[i]
}

func (c *cacheProbeLLM) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.users)
}

// 分章写作的每一章都必须复用同一份基线 + 参考知识的前缀。
//
// 回归目标：场面节拍曾留在 documents（只是按章替换内容），公共前缀因此停在
// documents 开头，32KB 的 references 全部按全价重算——4 章单元实测第 3、4 章
// 只命中 896 token。现在节拍走 extra，前缀必须覆盖到 instruction 结束。
func TestWriteChapterCallsShareTheBaselinePrefix(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	unit.ChapterFrom, unit.ChapterTo = 1, 3
	fake := &cacheProbeLLM{}
	r := New(fake)
	r.KnowledgeDocs = []string{
		strings.Repeat("# 犯罪人情味\n把案子写进人味里。", 400),
		strings.Repeat("# 去 AI 味\n删掉解释腔。", 400),
	}

	if _, err := r.RunWrite(context.Background(), snap, unit, "", "mock/mock-gpt-4"); err != nil {
		t.Fatal(err)
	}
	if got := fake.count(); got < 3 {
		t.Fatalf("calls=%d want >=3 (one per chapter)", got)
	}

	// 前缀至少要盖住 extra 之前的一切：documents、references、task、instruction。
	marker := `"extra":{`
	for i := 1; i < fake.count(); i++ {
		sPrev, uPrev := fake.prompts(i - 1)
		s, u := fake.prompts(i)
		if sPrev != s {
			t.Fatalf("call %d changed the system prompt; prefix cache would miss", i+1)
		}
		idx := strings.Index(u, marker)
		if idx < 0 {
			t.Fatalf("call %d payload has no extra block", i+1)
		}
		need := len(s) + idx + len(marker)
		shared := commonPrefixLen(sPrev+uPrev, s+u)
		if shared < need {
			t.Fatalf("call %d shares only %d B of %d B; the baseline is re-billed per chapter",
				i+1, shared, len(s+u))
		}
	}
}

// 「生成 → 硬门修复」这一层同样要复用生成时的前缀：修复只该在尾部追加诊断。
func TestChapterGateRepairReusesTheGenerationPrefix(t *testing.T) {
	snap, unit, _ := loadFixtureUnit(t)
	// 两章才会走分章写作（单章单元走"整单元一次写完"的路径）。
	unit.ChapterFrom, unit.ChapterTo = 1, 2
	fake := &cacheProbeLLM{firstPassShort: true}
	r := New(fake)
	r.KnowledgeDocs = []string{strings.Repeat("# 参考\n风格参考正文。", 400)}

	if _, err := r.RunWrite(context.Background(), snap, unit, "", "mock/mock-gpt-4"); err != nil {
		t.Fatal(err)
	}
	// 调用顺序：ch1 生成、ch2 生成、ch1 硬门修复（缺字的那一章才修）。
	if got := fake.count(); got < 3 {
		t.Fatalf("calls=%d want >=3 (2 generate + 1 gate repair)", got)
	}
	s0, u0 := fake.prompts(0)
	s2, u2 := fake.prompts(2)
	if s0 != s2 {
		t.Fatal("repair changed the system prompt; prefix cache would miss")
	}
	shared := commonPrefixLen(s0+u0, s2+u2)
	if ratio := float64(shared) / float64(len(s2+u2)); ratio < 0.8 {
		t.Fatalf("repair shares only %.1f%% of the prompt (%d/%d B); 诊断必须只在尾部追加",
			ratio*100, shared, len(s2+u2))
	}
}
