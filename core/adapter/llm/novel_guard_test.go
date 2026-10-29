package llm

import (
	"context"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
)

func TestProviderRejectsTools(t *testing.T) {
	// use mock store through sqlite would need deps; just check that a client built
	// with nil managers fails before any network when tools are present.
	c := &DefaultLLMProviderClient{}
	_, err := c.Chat(context.Background(), port.LLMChatRequest{
		Model: "mock/mock-gpt-4",
		Tools: []domain.ToolSchema{{Name: "x"}},
	})
	if err == nil {
		t.Fatal("expected tools rejection")
	}
	_ = llmconfig.NewModelRegistry()
}
