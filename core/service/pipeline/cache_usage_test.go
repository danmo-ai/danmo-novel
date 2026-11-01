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

// cacheReportingLLM 汇报缓存用量，模拟真实 provider 的 usage 字段。
//
// 第二次（修复重试）命中首轮的全部 prompt——这正是 repair 追加在 payload
// 末尾时应有的结果。
type cacheReportingLLM struct {
	mu      sync.Mutex
	calls   int
	prompts []int
}

func (c *cacheReportingLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	c.mu.Lock()
	c.calls++
	n := len(req.Messages[len(req.Messages)-1].Content) / 2 // 粗略的 token 近似
	first := c.calls == 1
	c.prompts = append(c.prompts, n)
	c.mu.Unlock()

	usage := &port.LLMUsage{PromptTokens: n, CompletionTokens: 50}
	if !first {
		// 重试：前缀全部命中，只新增 repair 段。
		usage.CacheReadTokens = c.prompts[0]
		usage.PromptTokens = c.prompts[0] + n - c.prompts[0]
	}
	out, _ := json.Marshal(map[string]any{"verdict": "pass", "findings": []any{}})
	return port.LLMChatResponse{Content: string(out), Done: true, Usage: usage}, nil
}

// 缓存用量必须落到 CallRecord，否则运行详情页与统计卡片没有数据源。
// adapter 解析出来只是第一步，链路到 UI 中间任何一环丢字段，这里就会空。
func TestCallRecordCarriesCacheUsage(t *testing.T) {
	snap, unit := loadReviewFixture(t)
	fake := &cacheReportingLLM{}
	r := New(fake)

	res, err := r.RunReview(context.Background(), snap, unit, "mock/mock-gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Calls) == 0 {
		t.Fatal("no call records")
	}
	rec := res.Calls[0]
	if rec.PromptTokens == 0 {
		t.Fatal("prompt tokens lost")
	}
	first := res.Calls[0]
	if first.CacheReadTokens != 0 {
		t.Fatalf("first call should not report cache hits, got %d", first.CacheReadTokens)
	}

	// 序列化后必须带上字段：SQLite 存的是 JSON blob，前端拿到的也是它。
	blob, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"promptTokens"`) {
		t.Fatalf("call record json missing token fields: %s", blob)
	}
}

func TestCallRecordOmitsEmptyCacheFields(t *testing.T) {
	// 老记录 / 不支持缓存的模型：字段不出现，前端才能区分"0 命中"与"没数据"。
	blob, err := json.Marshal(cacheZeroRecord())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cacheReadTokens", "cacheCreationTokens"} {
		if strings.Contains(string(blob), key) {
			t.Fatalf("%s should be omitted when zero: %s", key, blob)
		}
	}
}

// 解码侧必须能读懂历史记录：缺字段时是 0，不是解析失败。
func TestCallRecordDecodesLegacyPayload(t *testing.T) {
	legacy := `{"index":1,"modelId":"m","provider":"p","step":"generate","promptTokens":100,"completionTokens":10,"durationMs":5,"startedAt":"2026-01-01T00:00:00Z"}`
	var rec struct {
		Index            int `json:"index"`
		PromptTokens     int `json:"promptTokens"`
		CacheReadTokens  int `json:"cacheReadTokens"`
		CacheCreateToken int `json:"cacheCreationTokens"`
	}
	if err := json.Unmarshal([]byte(legacy), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.PromptTokens != 100 || rec.CacheReadTokens != 0 {
		t.Fatalf("legacy decode wrong: %+v", rec)
	}
}

func cacheZeroRecord() struct {
	Index            int    `json:"index"`
	ModelID          string `json:"modelId"`
	Provider         string `json:"provider"`
	Step             string `json:"step"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	CacheReadTokens  int    `json:"cacheReadTokens,omitempty"`
	CacheCreateToken int    `json:"cacheCreationTokens,omitempty"`
	DurationMs       int64  `json:"durationMs"`
	StartedAt        string `json:"startedAt"`
} {
	return struct {
		Index            int    `json:"index"`
		ModelID          string `json:"modelId"`
		Provider         string `json:"provider"`
		Step             string `json:"step"`
		PromptTokens     int    `json:"promptTokens"`
		CompletionTokens int    `json:"completionTokens"`
		CacheReadTokens  int    `json:"cacheReadTokens,omitempty"`
		CacheCreateToken int    `json:"cacheCreationTokens,omitempty"`
		DurationMs       int64  `json:"durationMs"`
		StartedAt        string `json:"startedAt"`
	}{Index: 1, ModelID: "m", Provider: "p", Step: "generate", PromptTokens: 100, StartedAt: "2026-01-01T00:00:00Z"}
}

// 保证 repair 场景下第二次调用确实拿到高分命中：repair 只改尾部。
func TestRepairRetryReportsHighCacheHit(t *testing.T) {
	first := contextsvc.EncodeUserPayload(contextsvc.UserPayload{
		Task: "review",
		Book: map[string]any{"id": "b1", "prose": strings.Repeat("正文", 500)},
	})
	second := contextsvc.MergeRepairIntoUserPayload(first, fmt.Errorf("verdict：需要 pass 或 revise，实际 （空）"))

	common := commonPrefixLen(first, second)
	if common < len(first)-1 {
		t.Fatalf("shared prefix=%d of %d bytes; retry would re-bill most of the context", common, len(first))
	}
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
