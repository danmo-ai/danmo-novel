package llmconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

type Store interface {
	GetAll(ctx context.Context) ([]domain.LLMProviderConfig, error)
	GetByID(ctx context.Context, id string) (domain.LLMProviderConfig, error)
	Upsert(ctx context.Context, cfg domain.LLMProviderConfig) error
	Delete(ctx context.Context, id string) error
}

type Manager struct {
	store    Store
	modelCfg *ModelRegistry
	mu       sync.RWMutex
	cache    map[string]domain.LLMProviderConfig
	loaded   bool
	presets  []domain.LLMProviderPreset
}

func NewManager(store Store, modelCfg *ModelRegistry) *Manager {
	return &Manager{store: store, modelCfg: modelCfg, presets: DefaultPresets()}
}

func (m *Manager) SetPresets(presets []domain.LLMProviderPreset) {
	if len(presets) > 0 {
		m.presets = presets
	}
}

func (m *Manager) Presets() []domain.LLMProviderPreset {
	out := make([]domain.LLMProviderPreset, len(m.presets))
	copy(out, m.presets)
	return out
}

func (m *Manager) ensureCache(ctx context.Context) {
	m.mu.RLock()
	if m.loaded {
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loaded {
		return
	}
	cfgs, err := m.store.GetAll(ctx)
	if err != nil {
		return
	}
	m.cache = make(map[string]domain.LLMProviderConfig, len(cfgs))
	for _, cfg := range cfgs {
		m.cache[cfg.ID] = cfg
	}
	m.loaded = true
}

func (m *Manager) GetAll(ctx context.Context) ([]domain.LLMProviderConfig, error) {
	m.ensureCache(ctx)
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.LLMProviderConfig, 0, len(m.cache))
	for _, cfg := range m.cache {
		out = append(out, cfg)
	}
	return out, nil
}

func (m *Manager) GetByID(ctx context.Context, id string) (domain.LLMProviderConfig, error) {
	m.ensureCache(ctx)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cache[id], nil
}

func (m *Manager) Upsert(ctx context.Context, req domain.UpsertLLMProviderConfigRequest) (domain.LLMProviderConfig, error) {
	if req.Provider == "" {
		return domain.LLMProviderConfig{}, fmt.Errorf("provider required")
	}
	if req.Name == "" {
		return domain.LLMProviderConfig{}, fmt.Errorf("name required")
	}
	m.ensureCache(ctx)
	now := time.Now().UTC().Format(time.RFC3339)

	m.mu.RLock()
	existing := m.cache[fmt.Sprintf("llm-%s", req.Name)]
	m.mu.RUnlock()

	defaultDialect := strings.TrimSpace(req.DefaultProtocolDialect)
	if defaultDialect == "" {
		defaultDialect = strings.TrimSpace(req.DefaultDialect)
	}

	apiKey := req.APIKey
	if apiKey == "" && existing.APIKey != "" {
		apiKey = existing.APIKey // blank means keep
	}

	cfg := domain.LLMProviderConfig{
		ID:                     existing.ID,
		Provider:               domain.NormalizeProtocol(req.Provider),
		Name:                   req.Name,
		APIKey:                 apiKey,
		BaseURL:                req.BaseURL,
		DefaultProtocolDialect: defaultDialect,
		Models:                 normalizeModelRefDialects(req.Models),
		UpdatedAt:              now,
	}
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("llm-%s", req.Name)
		cfg.CreatedAt = now
	}
	if err := m.store.Upsert(ctx, cfg); err != nil {
		return domain.LLMProviderConfig{}, err
	}
	m.mu.Lock()
	if m.cache == nil {
		m.cache = make(map[string]domain.LLMProviderConfig)
	}
	m.cache[cfg.ID] = cfg
	m.mu.Unlock()
	return cfg, nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	if err := m.store.Delete(ctx, id); err != nil {
		return err
	}
	m.mu.Lock()
	if m.cache != nil {
		delete(m.cache, id)
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) ListModels(ctx context.Context) []domain.LLMModel {
	m.ensureCache(ctx)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.LLMModel
	for _, cfg := range m.cache {
		for _, ref := range cfg.Models {
			if !ref.Enabled {
				continue
			}
			modelID := cfg.Name + "/" + ref.Name
			var efforts []string
			vision := false
			if m.modelCfg != nil {
				efforts = m.modelCfg.AvailableEfforts(modelID)
				vision = m.modelCfg.SupportsVision(modelID)
			}
			if len(efforts) == 0 {
				efforts = DefaultEffortsForProvider(cfg.Provider)
			}
			out = append(out, domain.LLMModel{
				ID:               modelID,
				Name:             ref.Name,
				ProviderID:       cfg.ID,
				Provider:         string(cfg.Provider),
				Enabled:          ref.Enabled,
				AvailableEfforts: efforts,
				Vision:           vision,
				ProtocolDialect:  domain.ResolveProtocolDialect(cfg, ref.Name),
			})
		}
	}
	return out
}

func (m *Manager) ResolveProvider(ctx context.Context, modelID string) (domain.LLMProviderConfig, string, error) {
	parts := strings.SplitN(modelID, "/", 3)
	if len(parts) < 2 {
		return domain.LLMProviderConfig{}, "", fmt.Errorf("invalid model identifier: %s, expected format: provider_name/model_name", modelID)
	}
	providerName := parts[0]
	modelName := parts[1]
	m.ensureCache(ctx)
	m.mu.RLock()
	cfg, ok := m.cache[fmt.Sprintf("llm-%s", providerName)]
	m.mu.RUnlock()
	if !ok || cfg.ID == "" {
		return domain.LLMProviderConfig{}, "", fmt.Errorf("provider not found: %s", providerName)
	}
	return cfg, modelName, nil
}

func (m *Manager) FetchModels(ctx context.Context, configID string) ([]domain.LLMModelRef, error) {
	m.ensureCache(ctx)
	m.mu.RLock()
	cfg, ok := m.cache[configID]
	m.mu.RUnlock()
	if !ok || cfg.ID == "" {
		return nil, fmt.Errorf("config not found: %s", configID)
	}
	remoteModels, err := listRemoteModels(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("fetch models from remote: %w", err)
	}
	existingByName := make(map[string]domain.LLMModelRef, len(cfg.Models))
	for _, ref := range cfg.Models {
		existingByName[ref.Name] = ref
	}
	now := time.Now().UTC().Format(time.RFC3339)
	merged := make([]domain.LLMModelRef, 0, len(remoteModels))
	for _, name := range remoteModels {
		prev, ok := existingByName[name]
		enabled := ok && prev.Enabled
		dialect := ""
		if ok {
			dialect = prev.ProtocolDialect
		}
		merged = append(merged, domain.LLMModelRef{
			Name: name, Enabled: enabled, ProtocolDialect: dialect,
		})
	}
	cfg.Models = merged
	cfg.UpdatedAt = now
	if err := m.store.Upsert(ctx, cfg); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.cache != nil {
		m.cache[cfg.ID] = cfg
	}
	m.mu.Unlock()
	return merged, nil
}

func (m *Manager) FetchModelsFromRequest(ctx context.Context, req domain.UpsertLLMProviderConfigRequest) ([]domain.LLMModelRef, error) {
	cfg := domain.LLMProviderConfig{
		Provider: domain.NormalizeProtocol(req.Provider),
		APIKey:   req.APIKey,
		BaseURL:  req.BaseURL,
	}
	remoteModels, err := listRemoteModels(ctx, cfg)
	if err != nil {
		return nil, err
	}
	refs := make([]domain.LLMModelRef, 0, len(remoteModels))
	for _, name := range remoteModels {
		refs = append(refs, domain.LLMModelRef{Name: name, Enabled: true})
	}
	return refs, nil
}

func (m *Manager) ToggleModel(ctx context.Context, configID, modelName string, enabled bool) (domain.LLMProviderConfig, error) {
	m.ensureCache(ctx)
	m.mu.RLock()
	cfg, ok := m.cache[configID]
	m.mu.RUnlock()
	if !ok || cfg.ID == "" {
		return domain.LLMProviderConfig{}, fmt.Errorf("config not found: %s", configID)
	}
	found := false
	for i, ref := range cfg.Models {
		if ref.Name == modelName {
			cfg.Models[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		return domain.LLMProviderConfig{}, fmt.Errorf("model not found: %s", modelName)
	}
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := m.store.Upsert(ctx, cfg); err != nil {
		return domain.LLMProviderConfig{}, err
	}
	m.mu.Lock()
	if m.cache != nil {
		m.cache[cfg.ID] = cfg
	}
	m.mu.Unlock()
	return cfg, nil
}

func normalizeModelRefDialects(refs []domain.LLMModelRef) []domain.LLMModelRef {
	if len(refs) == 0 {
		return refs
	}
	out := make([]domain.LLMModelRef, len(refs))
	copy(out, refs)
	for i := range out {
		out[i].ProtocolDialect = strings.TrimSpace(out[i].ProtocolDialect)
	}
	return out
}

func listRemoteModels(ctx context.Context, cfg domain.LLMProviderConfig) ([]string, error) {
	switch domain.NormalizeProtocol(cfg.Provider) {
	case domain.LLMProviderMock:
		return []string{"mock-gpt-4", "mock-claude"}, nil
	case domain.LLMProviderAnthropic:
		return nil, fmt.Errorf("Anthropic API does not expose a model list endpoint; configure models manually")
	case domain.LLMProviderOpenAI, domain.LLMProviderOpenAIResponses:
		return listOpenAICompatibleModels(ctx, cfg.BaseURL, cfg.APIKey)
	default:
		return nil, fmt.Errorf("unsupported LLM protocol %q", cfg.Provider)
	}
}

func listOpenAICompatibleModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	url := strings.TrimRight(baseURL, "/") + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	var names []string
	for _, m := range result.Data {
		if m.ID != "" {
			names = append(names, m.ID)
		}
	}
	return names, nil
}

func DefaultPresets() []domain.LLMProviderPreset {
	return []domain.LLMProviderPreset{
		{ID: "openai", Name: "OpenAI", Provider: domain.LLMProviderOpenAI, BaseURL: "https://api.openai.com/v1", DefaultProtocolDialect: domain.ProtocolDialectOpenAI, Description: "GPT 系列、o 系列推理模型"},
		{ID: "anthropic", Name: "Anthropic", Provider: domain.LLMProviderAnthropic, BaseURL: "https://api.anthropic.com/v1", Description: "Claude Sonnet、Opus、Haiku"},
		{ID: "deepseek", Name: "DeepSeek", Provider: domain.LLMProviderOpenAI, BaseURL: "https://api.deepseek.com/v1", DefaultProtocolDialect: domain.ProtocolDialectDeepSeek, Description: "DeepSeek：deepseek-flash、deepseek-v4-pro"},
		{ID: "google", Name: "Google Gemini", Provider: domain.LLMProviderOpenAI, BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", DefaultProtocolDialect: domain.ProtocolDialectGemini, Description: "Gemini Pro、Flash"},
		{ID: "zhipu", Name: "智谱 (Zhipu)", Provider: domain.LLMProviderOpenAI, BaseURL: "https://open.bigmodel.cn/api/paas/v4", DefaultProtocolDialect: domain.ProtocolDialectGLM, Description: "GLM 系列"},
		{ID: "qwen", Name: "通义千问 (Qwen)", Provider: domain.LLMProviderOpenAI, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", DefaultProtocolDialect: domain.ProtocolDialectQwen, Description: "Qwen 系列"},
		// Leave DefaultProtocolDialect empty so InferProtocolDialect can pick
		// kimi vs kimi_k3 vs kimi_code per model id (k3.1-pro, k2.6, …).
		{ID: "moonshot", Name: "Moonshot (Kimi)", Provider: domain.LLMProviderOpenAI, BaseURL: "https://api.kimi.com/coding/v1", Description: "Kimi Code：k3.1-pro、k3、k3-256k、kimi-for-coding"},
		{ID: "minimax", Name: "MiniMax", Provider: domain.LLMProviderOpenAI, BaseURL: "https://api.minimaxi.com/v1", DefaultProtocolDialect: domain.ProtocolDialectMiniMax, Description: "MiniMax M 系列"},
		{ID: "ollama", Name: "Ollama (Local)", Provider: domain.LLMProviderOpenAI, BaseURL: "http://localhost:11434/v1", Description: "本地模型"},
		{ID: "openrouter", Name: "OpenRouter", Provider: domain.LLMProviderOpenAI, BaseURL: "https://openrouter.ai/api/v1", Description: "多模型路由"},
		{ID: "xai", Name: "xAI", Provider: domain.LLMProviderOpenAI, BaseURL: "https://api.x.ai/v1", DefaultProtocolDialect: domain.ProtocolDialectGrok, Description: "Grok 系列"},
		{ID: "mock", Name: "Mock", Provider: domain.LLMProviderMock, BaseURL: "", Description: "本地测试用 mock"},
	}
}
