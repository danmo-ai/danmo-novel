package domain

import "testing"

func TestResolveProtocolDialect_ModelOverride(t *testing.T) {
	cfg := LLMProviderConfig{
		DefaultProtocolDialect: ProtocolDialectQwen,
		Models: []LLMModelRef{
			{Name: "custom-m", Enabled: true, ProtocolDialect: ProtocolDialectDeepSeek},
			{Name: "inherit-m", Enabled: true},
		},
	}
	if got := ResolveProtocolDialect(cfg, "custom-m"); got != ProtocolDialectDeepSeek {
		t.Fatalf("override: got %q", got)
	}
	if got := ResolveProtocolDialect(cfg, "inherit-m"); got != ProtocolDialectQwen {
		t.Fatalf("inherit provider default: got %q", got)
	}
}

func TestResolveProtocolDialect_InferFallback(t *testing.T) {
	cfg := LLMProviderConfig{
		Models: []LLMModelRef{{Name: "deepseek-flash", Enabled: true}},
	}
	if got := ResolveProtocolDialect(cfg, "deepseek-flash"); got != ProtocolDialectDeepSeek {
		t.Fatalf("infer: got %q", got)
	}
}

func TestResolveProtocolDialect_InferBeatsProviderDefault(t *testing.T) {
	// Moonshot footgun: provider default "kimi" must not pin K3.1 Pro to thinking.disabled.
	cfg := LLMProviderConfig{
		DefaultProtocolDialect: ProtocolDialectKimi,
		Models: []LLMModelRef{
			{Name: "k3.1-pro", Enabled: true},
			{Name: "k3-256k", Enabled: true},
		},
	}
	if got := ResolveProtocolDialect(cfg, "k3.1-pro"); got != ProtocolDialectKimiK3 {
		t.Fatalf("k3.1-pro: got %q want kimi_k3", got)
	}
	if got := ResolveProtocolDialect(cfg, "k3-256k"); got != ProtocolDialectKimiK3 {
		t.Fatalf("k3-256k: got %q want kimi_k3", got)
	}
}

func TestResolveProtocolDialect_OpenAIDefault(t *testing.T) {
	cfg := LLMProviderConfig{
		Models: []LLMModelRef{{Name: "unknown-model-xyz", Enabled: true}},
	}
	if got := ResolveProtocolDialect(cfg, "unknown-model-xyz"); got != ProtocolDialectOpenAI {
		t.Fatalf("default openai: got %q", got)
	}
}

func TestResolveProtocolDialect_MissingModelUsesProviderDefault(t *testing.T) {
	cfg := LLMProviderConfig{DefaultProtocolDialect: ProtocolDialectGLM}
	if got := ResolveProtocolDialect(cfg, "not-listed"); got != ProtocolDialectGLM {
		t.Fatalf("missing model: got %q", got)
	}
}
