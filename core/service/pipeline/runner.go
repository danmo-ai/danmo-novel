package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
)

// chatLLM is deliberately protocol-agnostic. The task runner owns codecs,
// validation and repair; the provider only transports system + user text.
func chatLLM(ctx context.Context, llm port.LLMProvider, modelID, system, user string, policy domain.CallPolicy) (string, domain.CallRecord, error) {
	start := time.Now()
	rec := domain.CallRecord{ModelID: modelID, Step: "chat", StartedAt: start}
	if i := strings.IndexByte(modelID, '/'); i > 0 {
		rec.Provider = modelID[:i]
	}
	resp, err := llm.Chat(ctx, port.LLMChatRequest{
		Model:     modelID,
		Messages:  []port.ChatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		GenParams: &port.ModelGenParams{MaxTokens: policy.MaxOutputTokens},
	})
	rec.DurationMs = time.Since(start).Milliseconds()
	// 停止原因先记下来：截断判定只看这个字段，不看我们发出去的额度。
	rec.FinishReason = resp.FinishReason
	if resp.Usage != nil {
		rec.PromptTokens = resp.Usage.PromptTokens
		rec.CompletionTokens = resp.Usage.CompletionTokens
		rec.CacheReadTokens = resp.Usage.CacheReadTokens
		rec.CacheCreationTokens = resp.Usage.CacheCreationTokens
	}
	if err != nil {
		rec.Error = sanitizeErr(err)
		return "", rec, err
	}
	if len(resp.ToolCalls) > 0 {
		rec.Error = "model returned tool calls which are forbidden"
		return "", rec, fmt.Errorf("%s", rec.Error)
	}
	return resp.Content, rec, nil
}
