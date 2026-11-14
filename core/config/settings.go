package config

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
)

type Settings struct {
	LibraryRoot       string                   `json:"libraryRoot"`
	ModelAssignments  []domain.ModelAssignment `json:"modelAssignments"`
	DefaultModels     []domain.ModelConfig     `json:"defaultModels"`
	LLMHTTPTimeoutSec int                      `json:"llmHttpTimeoutSec"`
	Review            ReviewSettings           `json:"review"`
}

// ReviewSettings controls bounded parallel per-chapter review.
type ReviewSettings struct {
	Concurrency int    `json:"concurrency"` // default 3
	Strictness  string `json:"strictness"`  // "loose" | "standard" | "strict"，default standard
	// Rounds 是每章独立审校的轮数，默认 3，取并集。
	//
	// 单轮审校的覆盖率有硬上限：同一份正文跑 4 轮，并集 128 处里单轮平均只看到
	// 25.4%，k=2 到 50.5%，k=3 到 75.4%。这不是审校不够深，而是每轮注意力落点
	// 不同（temperature=0 也无法固定）。所以提覆盖率唯一的办法是多轮取并集。
	//
	// 成本：轮数只让「输出」翻倍（思维链），输入靠前缀缓存几乎不变。3 轮约让
	// 单章成本从 $0.0104 涨到 $0.0140（+35%），而不是 +200%。
	// 设为 1 即退回单轮行为。
	Rounds int `json:"rounds"` // default 3
}

type Manager struct {
	mu       sync.RWMutex
	settings Settings
	path     string
}

func Load() (*Manager, error) {
	if err := runtimepaths.EnsureDirs(); err != nil {
		return nil, err
	}
	m := &Manager{path: runtimepaths.SettingsPath()}
	m.settings = Settings{
		LibraryRoot: runtimepaths.LibraryRoot(),
		ModelAssignments: []domain.ModelAssignment{
			{Role: domain.StagePlan, ModelID: ""},
			{Role: domain.StageOutline, ModelID: ""},
			{Role: domain.StageWrite, ModelID: ""},
			{Role: domain.StageReview, ModelID: ""},
			{Role: domain.StageFinalize, ModelID: ""},
			{Role: domain.StageLocalEdit, ModelID: ""},
		},
		LLMHTTPTimeoutSec: 600,
		Review: ReviewSettings{
			Concurrency: 3,
			Strictness:  "standard",
			// 默认 3 轮取并集：单轮只覆盖约 31% 的问题，是抽样不确定性而非
			// 审校深度，所以必须多轮。3 轮把覆盖率提到约 75%。
			Rounds: 3,
		},
	}
	b, err := os.ReadFile(m.path)
	if err == nil {
		_ = json.Unmarshal(b, &m.settings)
	}
	// 旧配置没有 rounds 字段，反序列化后是 0：不能当成"跑 0 轮"。
	if m.settings.Review.Rounds <= 0 {
		m.settings.Review.Rounds = 3
	}
	if m.settings.LibraryRoot == "" {
		m.settings.LibraryRoot = runtimepaths.LibraryRoot()
	}
	// The embedded development workspace always uses the current Home. There
	// is no legacy absolute-path migration or external library compatibility.
	if os.Getenv("NOVEL_HOME") != "" {
		m.settings.LibraryRoot = runtimepaths.LibraryRoot()
	}
	if len(m.settings.DefaultModels) == 0 {
		m.settings.DefaultModels = llmconfig.BuiltInDefaultModels()
	}
	return m, m.Save()
}

func (m *Manager) Get() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

func (m *Manager) Update(fn func(*Settings)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.settings)
	return m.saveLocked()
}

func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked()
}

func (m *Manager) saveLocked() error {
	b, err := json.MarshalIndent(m.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0o600)
}

func (m *Manager) ModelFor(role domain.StageModelRole) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.settings.ModelAssignments {
		if a.Role == role && a.ModelID != "" {
			return a.ModelID
		}
	}
	return ""
}
