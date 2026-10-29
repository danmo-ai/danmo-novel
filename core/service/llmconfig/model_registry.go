package llmconfig

import (
	"strings"
	"sync"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
)

type ModelRegistry struct {
	mu      sync.RWMutex
	config  []domain.ModelConfig
	byModel map[string]domain.ModelConfig
}

func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{}
}

func (r *ModelRegistry) SetModels(models []domain.ModelConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = models
	r.byModel = nil
}

func (r *ModelRegistry) ensureIndex() {
	if r.byModel != nil {
		return
	}
	r.byModel = make(map[string]domain.ModelConfig, len(r.config))
	for _, l := range r.config {
		r.byModel[strings.ToLower(l.Model)] = l
	}
}

func (r *ModelRegistry) lookup(modelID string) *domain.ModelConfig {
	_, model := splitModelID(modelID)
	if model == "" {
		return nil
	}
	r.mu.RLock()
	r.ensureIndex()
	defer r.mu.RUnlock()
	key := strings.ToLower(modelID)
	if c, ok := r.byModel[key]; ok {
		return &c
	}
	if c, ok := r.byModel[strings.ToLower(model)]; ok {
		return &c
	}
	return nil
}

func (r *ModelRegistry) AvailableEfforts(modelID string) []string {
	if c := r.lookup(modelID); c != nil && len(c.AvailableEfforts) > 0 {
		return c.AvailableEfforts
	}
	return nil
}

func DefaultEffortsForProvider(provider domain.LLMProviderType) []string {
	switch domain.NormalizeProtocol(provider) {
	case domain.LLMProviderAnthropic:
		return append([]string(nil), domain.DefaultEffortsAnthropic...)
	case domain.LLMProviderOpenAI, domain.LLMProviderOpenAIResponses:
		return append([]string(nil), domain.DefaultEffortsOpenAI...)
	default:
		return nil
	}
}

func (r *ModelRegistry) SupportsVision(modelID string) bool {
	if c := r.lookup(modelID); c != nil {
		return c.Vision
	}
	return false
}

func (r *ModelRegistry) ContextWindow(modelID string) int {
	if c := r.lookup(modelID); c != nil && c.ContextWindow > 0 {
		return c.ContextWindow
	}
	return 128_000
}

func (r *ModelRegistry) MaxOutputTokens(modelID string) int {
	if c := r.lookup(modelID); c != nil && c.MaxOutput > 0 {
		return c.MaxOutput
	}
	return 8_192
}

func (r *ModelRegistry) GenParams(modelID string) *port.ModelGenParams {
	c := r.lookup(modelID)
	if c == nil {
		return nil
	}
	p := &port.ModelGenParams{
		MaxTokens:        c.MaxOutput,
		Temperature:      c.Temperature,
		TopP:             c.TopP,
		FrequencyPenalty: c.FrequencyPenalty,
		PresencePenalty:  c.PresencePenalty,
		Stop:             c.Stop,
	}
	if p.MaxTokens == 0 && p.Temperature == 0 && p.TopP == 0 &&
		p.FrequencyPenalty == 0 && p.PresencePenalty == 0 &&
		len(p.Stop) == 0 {
		return nil
	}
	return p
}

func (r *ModelRegistry) AllModels() []domain.ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.ModelConfig, len(r.config))
	copy(out, r.config)
	return out
}

func splitModelID(modelID string) (provider, model string) {
	parts := strings.SplitN(modelID, "/", 3)
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", modelID
}
