package config

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
)

type Settings struct {
	LibraryRoot      string                   `json:"libraryRoot"`
	ModelAssignments []domain.ModelAssignment `json:"modelAssignments"`
	DefaultModels    []domain.ModelConfig     `json:"defaultModels"`
	LLMHTTPTimeoutSec int                     `json:"llmHttpTimeoutSec"`
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
			{Role: domain.StageLocalEdit, ModelID: ""},
		},
		LLMHTTPTimeoutSec: 600,
	}
	b, err := os.ReadFile(m.path)
	if err == nil {
		_ = json.Unmarshal(b, &m.settings)
	}
	if m.settings.LibraryRoot == "" {
		m.settings.LibraryRoot = runtimepaths.LibraryRoot()
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
