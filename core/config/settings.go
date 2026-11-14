package config

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
)

type Settings struct {
	// LibraryRoot is the book library this process reads. It is derived on every
	// load from CustomLibraryRoot and the platform default, so the stored value
	// is never trusted — switching hosts (or the iCloud container appearing) can
	// never strand the app on a directory that no longer applies.
	LibraryRoot string `json:"libraryRoot"`
	// CustomLibraryRoot is the author's explicit choice. Empty means "follow the
	// platform default", which is what makes macOS pick up iCloud when the
	// container appears, without a stored value pinning the old local library.
	CustomLibraryRoot string                   `json:"customLibraryRoot,omitempty"`
	ModelAssignments  []domain.ModelAssignment `json:"modelAssignments"`
	DefaultModels     []domain.ModelConfig     `json:"defaultModels"`
	LLMHTTPTimeoutSec int                      `json:"llmHttpTimeoutSec"`
	Review            ReviewSettings           `json:"review"`
	// UpdatedAt is the last time the syncable block (assignments, model
	// configs, review) changed. Cross-device merge takes the newer block whole;
	// see core/service/settingsync.
	UpdatedAt string `json:"updatedAt,omitempty"`
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
		LibraryRoot: runtimepaths.DefaultLibraryRoot(),
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
	// The effective root is always recomputed: CustomLibraryRoot is the only
	// persisted choice, and an empty one follows the platform default so macOS
	// adopts iCloud as soon as the container exists.
	m.settings.LibraryRoot = effectiveLibraryRoot(m.settings.CustomLibraryRoot)
	if len(m.settings.DefaultModels) == 0 {
		m.settings.DefaultModels = llmconfig.BuiltInDefaultModels()
	}
	// The chosen directory may not exist yet (fresh install, external volume).
	// Creating it here covers the common case; a volume that is not mounted must
	// not stop the app from starting — the first write reports the real error
	// and the author can fix the path in Settings.
	_ = os.MkdirAll(m.settings.LibraryRoot, 0o755)
	return m, m.Save()
}

func effectiveLibraryRoot(custom string) string {
	if custom != "" {
		return custom
	}
	return runtimepaths.DefaultLibraryRoot()
}

// SetLibraryRoot points this process at a new book directory.
//
// It deliberately does not stamp UpdatedAt: a directory is a per-device fact
// (a phone's container path means nothing on a Mac) and must never be merged
// into the shared snapshot. The caller is responsible for having moved the
// files; this only records the choice.
func (m *Manager) SetLibraryRoot(custom string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings.CustomLibraryRoot = custom
	m.settings.LibraryRoot = effectiveLibraryRoot(custom)
	return m.saveLocked()
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
	// Every Update caller edits a synced block (assignments / model configs /
	// review), so stamping here is the single place the merge clock advances.
	m.settings.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return m.saveLocked()
}

// ApplyRemote replaces the synced block with a remote snapshot. It deliberately
// does not stamp UpdatedAt: the remote value is the clock for this block, and
// re-stamping would make every pull look like a local edit and push it back.
func (m *Manager) ApplyRemote(assignments []domain.ModelAssignment, models []domain.ModelConfig, review ReviewSettings, updatedAt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if assignments != nil {
		m.settings.ModelAssignments = assignments
	}
	if models != nil {
		m.settings.DefaultModels = models
	}
	// A zero value means the snapshot predates the field, not that the author
	// asked for zero rounds. Keep the local value rather than disabling review.
	if review.Concurrency > 0 {
		m.settings.Review.Concurrency = review.Concurrency
	}
	if review.Rounds > 0 {
		m.settings.Review.Rounds = review.Rounds
	}
	if review.Strictness != "" {
		m.settings.Review.Strictness = review.Strictness
	}
	m.settings.UpdatedAt = updatedAt
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
