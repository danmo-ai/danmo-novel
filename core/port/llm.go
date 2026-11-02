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
	PromptTokens     int
	CompletionTokens int
	// ReasoningTokens is the思考 portion of CompletionTokens.
	//
	// 它已经包含在 CompletionTokens 里，provider 计费也按 completion 计。单独
	// 记一份是因为两者的量级差得离谱：实测一次单章审校 completion=4997，
	// findings JSON 只占约 530，其余 4460 全是思维链。不分开记，作者会以为
	// "审校输出的问题清单很贵"，从而去压缩本来就已很小的 findings 规模。
	ReasoningTokens     int
	TotalTokens         int
	CacheReadTokens     int
	CacheCreationTokens int
}

func (u *LLMUsage) Empty() bool {
	if u == nil {
		return true
	}
	return u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 &&
		u.ReasoningTokens == 0 && u.CacheReadTokens == 0 && u.CacheCreationTokens == 0
}

type LLMChatResponse struct {
	Content            string
	ReasoningContent   string
	ReasoningSignature string
	ReasoningRedacted  string
	ToolCalls          []ChatToolCall
	Usage              *LLMUsage
	Done               bool
	// FinishReason 是 provider 自己说的「为什么停」：stop / length / tool_calls /
	// content_filter，未提供时为空。以前这里没有任何停止原因，于是"输出被截断"
	// 只能靠 completion_tokens 是否恰好等于预算来间接猜——实测报 8191 / 8192
	// 的差别就决定了一次重试能不能拿到更大的预算。
	FinishReason string
}

// FinishReasonLength 是所有方言对「撞到输出上限」的统一取值。
const FinishReasonLength = "length"

// LLMProvider is the only LLM surface domain code may call.
type LLMProvider interface {
	Chat(ctx context.Context, req LLMChatRequest) (LLMChatResponse, error)
}
