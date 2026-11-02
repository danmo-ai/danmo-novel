package llm

import (
	"encoding/json"
	"testing"
)

// 审校的实际形态是「答案很短、思考很长」：一次单章审校 completion=4997，
// findings JSON 只占约 530，其余全是思维链。provider 已经把这个数报回来了
// （completion_tokens_details.reasoning_tokens），丢掉它就只能看到一个巨大的
// completion，无从判断成本花在思考还是答案上。
func TestUsageParsesReasoningTokens(t *testing.T) {
	raw := `{
		"prompt_tokens": 9176,
		"completion_tokens": 4997,
		"total_tokens": 14173,
		"prompt_tokens_details": {"cached_tokens": 8960},
		"completion_tokens_details": {"reasoning_tokens": 4463, "text_tokens": 534}
	}`
	var u openaiCompatibleUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	got := u.toLLMUsage()
	if got == nil {
		t.Fatal("usage should not be empty")
	}
	if got.ReasoningTokens != 4463 {
		t.Fatalf("reasoningTokens=%d want 4463", got.ReasoningTokens)
	}
	// 思维链已经包含在 completion 里，不能再加一次。
	if got.CompletionTokens != 4997 {
		t.Fatalf("completionTokens=%d want 4997 (reasoning is a subset)", got.CompletionTokens)
	}
	if got.CacheReadTokens != 8960 {
		t.Fatalf("cacheReadTokens=%d want 8960", got.CacheReadTokens)
	}
}

// Responses API 把同一个数字放在 output_tokens_details 下。
func TestUsageParsesReasoningTokensFromResponsesShape(t *testing.T) {
	raw := `{
		"input_tokens": 8000,
		"output_tokens": 1200,
		"total_tokens": 9200,
		"output_tokens_details": {"reasoning_tokens": 900}
	}`
	var u openaiCompatibleUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	got := u.toLLMUsage()
	if got == nil {
		t.Fatal("usage should not be empty")
	}
	if got.PromptTokens != 8000 || got.CompletionTokens != 1200 {
		t.Fatalf("prompt=%d completion=%d", got.PromptTokens, got.CompletionTokens)
	}
	if got.ReasoningTokens != 900 {
		t.Fatalf("reasoningTokens=%d want 900", got.ReasoningTokens)
	}
}

// 不报思维链的 provider（Anthropic 把 thinking 算进 output，不单列）留 0，
// 而不是编一个猜出来的数字。
func TestUsageLeavesReasoningZeroWhenAbsent(t *testing.T) {
	raw := `{"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150}`
	var u openaiCompatibleUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	got := u.toLLMUsage()
	if got.ReasoningTokens != 0 {
		t.Fatalf("reasoningTokens=%d want 0", got.ReasoningTokens)
	}
}
