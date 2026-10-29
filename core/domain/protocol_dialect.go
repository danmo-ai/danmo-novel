package domain

import "strings"

// ResolveProtocolDialect picks the Chat Completions protocol dialect for a
// provider×model pair: model row override → Infer(model) → provider
// DefaultProtocolDialect → openai. Infer wins over provider default so mixed
// Moonshot catalogs (kimi vs kimi_k3 vs kimi_code) are not pinned by a stale
// provider dialect. Only meaningful for protocol openai.
func ResolveProtocolDialect(cfg LLMProviderConfig, modelName string) string {
	modelName = strings.TrimSpace(modelName)
	for _, m := range cfg.Models {
		if m.Name != modelName {
			continue
		}
		if d := strings.TrimSpace(m.ProtocolDialect); d != "" {
			return d
		}
		if d := InferProtocolDialect(modelName); d != "" {
			return d
		}
		if d := strings.TrimSpace(cfg.DefaultProtocolDialect); d != "" {
			return d
		}
		return ProtocolDialectOpenAI
	}
	if d := InferProtocolDialect(modelName); d != "" {
		return d
	}
	if d := strings.TrimSpace(cfg.DefaultProtocolDialect); d != "" {
		return d
	}
	return ProtocolDialectOpenAI
}

// InferProtocolDialect guesses Chat Completions protocol dialect from a model id
// (may be "provider/model" or bare model name). Empty if unknown.
// Order matches core/adapter/llm/PROTOCOL_DIALECTS.md.
func InferProtocolDialect(modelID string) string {
	s := strings.ToLower(modelID)
	base := s
	if i := strings.LastIndex(s, "/"); i >= 0 {
		base = s[i+1:]
	}
	switch {
	case strings.Contains(s, "deepseek"):
		return ProtocolDialectDeepSeek
	case strings.Contains(s, "qwen"):
		return ProtocolDialectQwen
	// K3 / K3.1 Pro / K2.8 Preview before other kimi* ids (reasoning_effort API).
	// k3.1-pro uses a dot; k3-256k uses a hyphen — match both prefixes.
	case base == "k3",
		base == "k3-256k",
		base == "kimi-for-coding",
		strings.Contains(s, "kimi-k3"),
		strings.Contains(s, "kimi-k2.8"),
		strings.HasPrefix(base, "k3-"),
		strings.HasPrefix(base, "k3."):
		return ProtocolDialectKimiK3
	// K2.7 Code / HighSpeed — always-on thinking object; disabled is rejected.
	case strings.Contains(s, "kimi-k2.7"),
		strings.Contains(s, "kimi-for-coding-highspeed"):
		return ProtocolDialectKimiCode
	case strings.Contains(s, "kimi"), strings.Contains(s, "moonshot"):
		return ProtocolDialectKimi
	case strings.Contains(s, "glm"), strings.Contains(s, "zhipu"), strings.Contains(s, "chatglm"):
		return ProtocolDialectGLM
	case strings.Contains(s, "minimax"):
		return ProtocolDialectMiniMax
	case strings.Contains(s, "gemini"):
		return ProtocolDialectGemini
	case strings.Contains(s, "grok"):
		return ProtocolDialectGrok
	default:
		return ""
	}
}
