package port

import (
	"context"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

type ChatContentPart struct {
	Type     string // "text" | "image"
	Text     string
	MimeType string
	Data     string
	Name     string
}

type ChatMessage struct {
	Role               string
	Content            string
	Parts              []ChatContentPart
	ToolCalls          []ChatToolCall
	ToolCallID         string
	Name               string
	ReasoningContent   string
	ReasoningSignature string
	ReasoningRedacted  string
}

type ChatToolCall struct {
	ID           string
	Name         string
	Arguments    map[string]any
	RepairedFrom string
}

type LLMChatRequest struct {
	Model      string
	Messages   []ChatMessage
	Tools      []domain.ToolSchema
	ToolChoice string
	GenParams  *ModelGenParams
}

type ModelGenParams struct {
	MaxTokens          int
	Temperature        float64
	TopP               float64
	FrequencyPenalty   float64
	PresencePenalty    float64
	Stop               []string
	ThinkingMode       string
	EffortBudgetTokens map[string]int
	ProtocolDialect    string
}

type LLMUsage struct {
	PromptTokens        int
	CompletionTokens    int
	TotalTokens         int
	CacheReadTokens     int
	CacheCreationTokens int
}

func (u *LLMUsage) Empty() bool {
	if u == nil {
		return true
	}
	return u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 &&
		u.CacheReadTokens == 0 && u.CacheCreationTokens == 0
}

type LLMChatResponse struct {
	Content            string
	ReasoningContent   string
	ReasoningSignature string
	ReasoningRedacted  string
	ToolCalls          []ChatToolCall
	Usage              *LLMUsage
	Done               bool
}

// LLMProvider is the only LLM surface domain code may call.
type LLMProvider interface {
	Chat(ctx context.Context, req LLMChatRequest) (LLMChatResponse, error)
}
