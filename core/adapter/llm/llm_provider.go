package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
)

type DefaultLLMProviderClient struct {
	mgr      *llmconfig.Manager
	modelCfg *llmconfig.ModelRegistry
	timeout  time.Duration
}

func NewDefaultLLMProvider(mgr *llmconfig.Manager, modelCfg *llmconfig.ModelRegistry) port.LLMProvider {
	return &DefaultLLMProviderClient{mgr: mgr, modelCfg: modelCfg, timeout: DefaultChatHTTPTimeout}
}

func (c *DefaultLLMProviderClient) WithTimeout(d time.Duration) *DefaultLLMProviderClient {
	if d > 0 {
		c.timeout = d
	}
	return c
}

func (c *DefaultLLMProviderClient) chatTimeout() time.Duration {
	if c.timeout > 0 {
		return c.timeout
	}
	return DefaultChatHTTPTimeout
}

func (c *DefaultLLMProviderClient) Chat(ctx context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	// Novel pipelines must never expose tools — refuse accidental tool schemas.
	if len(req.Tools) > 0 {
		return port.LLMChatResponse{}, fmt.Errorf("tools are not allowed in danmo-novel LLM calls")
	}
	req.Tools = nil
	req.ToolChoice = ""

	providerName, modelName, effort := ParseModelID(req.Model)
	if providerName == "" {
		return port.LLMChatResponse{}, fmt.Errorf("model not specified or invalid format (expected provider/model)")
	}

	cfg, mn, err := c.mgr.ResolveProvider(ctx, providerName+"/"+modelName)
	if err != nil {
		return port.LLMChatResponse{}, err
	}
	if mn == "" {
		mn = modelName
	}
	req.Model = mn

	modelID := providerName + "/" + modelName
	if c.modelCfg == nil || !c.modelCfg.SupportsVision(modelID) {
		req.Messages = stripImageParts(req.Messages)
	}

	if c.modelCfg != nil {
		req.GenParams = mergeGenParams(req.GenParams, c.modelCfg.GenParams(modelID))
	}
	if req.GenParams == nil {
		req.GenParams = &port.ModelGenParams{}
	}

	protocol := domain.NormalizeProtocol(cfg.Provider)
	if protocol == domain.LLMProviderOpenAI || protocol == domain.LLMProviderLocal {
		if req.GenParams.ProtocolDialect == "" {
			req.GenParams.ProtocolDialect = domain.ResolveProtocolDialect(cfg, modelName)
		}
		if len(req.GenParams.EffortBudgetTokens) == 0 {
			req.GenParams.EffortBudgetTokens = defaultEffortBudgetTokens(req.GenParams.ProtocolDialect)
		}
	}

	var effortCfg *EffortConfig
	if protocol == domain.LLMProviderAnthropic {
		mode := req.GenParams.ThinkingMode
		if mode == "" {
			mode = "adaptive"
		}
		budgets := req.GenParams.EffortBudgetTokens
		if len(budgets) == 0 {
			budgets = defaultAnthropicEffortBudgets()
		}
		effortCfg = &EffortConfig{
			ThinkingMode:       mode,
			EffortBudgetTokens: budgets,
		}
	}

	timeout := c.chatTimeout()
	switch protocol {
	case domain.LLMProviderAnthropic:
		return NewAnthropicMessagesClientWithTimeout(cfg.BaseURL, cfg.APIKey, timeout).Chat(ctx, req, effort, effortCfg)
	case domain.LLMProviderMock:
		return NewMock().Chat(ctx, req)
	case domain.LLMProviderOpenAI:
		return NewOpenAIChatCompletionsClientWithTimeout(cfg.BaseURL, cfg.APIKey, timeout).Chat(ctx, req, effort)
	case domain.LLMProviderOpenAIResponses:
		return NewOpenAIResponsesClientWithTimeout(cfg.BaseURL, cfg.APIKey, timeout).Chat(ctx, req, effort)
	default:
		return port.LLMChatResponse{}, fmt.Errorf("unsupported LLM protocol %q", cfg.Provider)
	}
}

func ParseModelID(modelID string) (providerName, modelName, effort string) {
	parts := strings.SplitN(modelID, "/", 3)
	if len(parts) >= 1 {
		providerName = parts[0]
	}
	if len(parts) >= 2 {
		modelName = parts[1]
	}
	if len(parts) >= 3 {
		effort = parts[2]
	}
	return
}

// mergeGenParams 用模型目录里的参数补全调用方的请求，并保证不超过模型自身上限。
//
// MaxTokens 的语义是「上层愿意给多少」，0 表示不设限、交给模型配置决定。
// 调用方没写时就用目录里的 max_output：一个声明 384000 的模型不该被上层按 8k 用。
// 调用方写了更大的值也不会真的发出去，仍夹回目录值。
func mergeGenParams(req, registry *port.ModelGenParams) *port.ModelGenParams {
	if registry == nil {
		return req
	}
	if req == nil {
		return registry
	}
	gp := *req
	rp := registry
	switch {
	case gp.MaxTokens == 0:
		gp.MaxTokens = rp.MaxTokens
	case rp.MaxTokens > 0 && gp.MaxTokens > rp.MaxTokens:
		gp.MaxTokens = rp.MaxTokens
	}
	if gp.Temperature == 0 {
		gp.Temperature = rp.Temperature
	}
	if gp.TopP == 0 {
		gp.TopP = rp.TopP
	}
	if gp.FrequencyPenalty == 0 {
		gp.FrequencyPenalty = rp.FrequencyPenalty
	}
	if gp.PresencePenalty == 0 {
		gp.PresencePenalty = rp.PresencePenalty
	}
	if len(gp.Stop) == 0 {
		gp.Stop = rp.Stop
	}
	return &gp
}

func stripImageParts(msgs []port.ChatMessage) []port.ChatMessage {
	out := make([]port.ChatMessage, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if len(m.Parts) > 0 {
			out[i].Parts = nil
		}
	}
	return out
}

func defaultEffortBudgetTokens(dialect string) map[string]int {
	switch dialect {
	case domain.ProtocolDialectQwen:
		return map[string]int{
			"low":    4096,
			"medium": 16384,
			"high":   32768,
			"xhigh":  262144,
			"max":    262144,
		}
	default:
		return nil
	}
}

func defaultAnthropicEffortBudgets() map[string]int {
	return map[string]int{
		"high": 16000,
		"max":  32000,
	}
}
