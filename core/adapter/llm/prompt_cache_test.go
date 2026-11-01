package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/port"
)

// 切分只允许发生在 block 边界：两个 block 被 Anthropic 无缝拼接，
// 所以拼接结果必须与输入逐字节相等，否则模型看到的是坏 JSON。
func TestSplitPromptCachePrefixIsByteExact(t *testing.T) {
	content := `{"task":"write","book":{"id":"b1","prose":"` + strings.Repeat("正文", 200) + `"},"extra":{"chapter":3},"repair":{"message":"chapters[1].body 为空"}}`
	stable, tail, ok := splitPromptCachePrefix(content)
	if !ok {
		t.Fatal("expected a split point before the repair block")
	}
	if stable+tail != content {
		t.Fatal("split is not byte-exact; the model would see a broken payload")
	}
	if !strings.HasPrefix(tail, promptCacheRepairMarker) {
		t.Fatalf("tail does not start at the repair block: %.40q", tail)
	}
	// 稳定前缀本身必须是合法 JSON 的"未闭合"形式：可解析性由拼接后保证，
	// 但它不能含有 repair 的键，否则断点位置就错了。
	if strings.Contains(stable, `"repair"`) {
		t.Fatal("stable prefix still contains the repair key")
	}
	if !json.Valid([]byte(stable + "}")) {
		t.Fatalf("stable prefix + closing brace is not valid JSON: %s", stable)
	}
}

func TestSplitPromptCachePrefixSkipsFirstAttempt(t *testing.T) {
	// 首轮 payload 没有 repair：不切分，交给 system 断点。
	content := `{"task":"review","book":{"id":"b1"}}`
	if _, _, ok := splitPromptCachePrefix(content); ok {
		t.Fatal("first attempt should not be split")
	}
	// 不是受控 payload 的普通文本也不该被切成两半。
	if _, _, ok := splitPromptCachePrefix("请改写这一段：他说,\"repair\": {}"); ok {
		t.Fatal("non-JSON content should not be split")
	}
}

// Anthropic 只在带 cache_control 的块上建立断点。user 侧的断点必须落在
// repair 之前，否则重试时正文仍在断点之后，等于没缓存。
func TestAnthropicPutsCacheBreakpointBeforeRepair(t *testing.T) {
	prose := strings.Repeat("雨落在青石上，他握紧了刀柄。", 80)
	payload := `{"task":"write","book":{"id":"b1","prose":"` + prose + `"},"repair":{"message":"body 为空"}}`

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("unmarshal request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 10, "output_tokens": 2},
		})
	}))
	defer server.Close()

	p := NewAnthropicMessagesClient(server.URL, "k")
	if _, err := p.Chat(context.Background(), port.LLMChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []port.ChatMessage{
			{Role: "system", Content: "你是小说编辑。"},
			{Role: "user", Content: payload},
		},
	}, "", nil); err != nil {
		t.Fatal(err)
	}

	messages, _ := gotBody["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages=%d want 1", len(messages))
	}
	msg, _ := messages[0].(map[string]any)
	blocks, ok := msg["content"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("user content should be split into 2 blocks, got %#v", msg["content"])
	}
	stable, _ := blocks[0].(map[string]any)
	tail, _ := blocks[1].(map[string]any)
	if _, has := stable["cache_control"]; !has {
		t.Fatal("stable prefix block must carry cache_control")
	}
	if _, has := tail["cache_control"]; has {
		t.Fatal("repair tail must stay outside the cache breakpoint")
	}
	joined := stable["text"].(string) + tail["text"].(string)
	if joined != payload {
		t.Fatal("blocks do not reassemble to the original payload")
	}
}

// 首轮（无 repair）不该被切分，也不能引入多出来的 block。
func TestAnthropicLeavesFirstAttemptUnsplitted(t *testing.T) {
	payload := `{"task":"write","book":{"id":"b1"}}`
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 10, "output_tokens": 2},
		})
	}))
	defer server.Close()

	p := NewAnthropicMessagesClient(server.URL, "k")
	if _, err := p.Chat(context.Background(), port.LLMChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []port.ChatMessage{
			{Role: "system", Content: "你是小说编辑。"},
			{Role: "user", Content: payload},
		},
	}, "", nil); err != nil {
		t.Fatal(err)
	}
	messages, _ := gotBody["messages"].([]any)
	msg, _ := messages[0].(map[string]any)
	if _, ok := msg["content"].([]any); ok {
		t.Fatalf("first attempt should stay a plain string, got %#v", msg["content"])
	}
	if msg["content"] != payload {
		t.Fatalf("content changed: %#v", msg["content"])
	}
}
