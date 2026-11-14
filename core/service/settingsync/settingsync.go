// Package settingsync shares model settings between devices through the folder
// the host app mounts from iCloud.
//
// The book library is shared as plain files and needs no service. Model
// settings are different: they live in SQLite, and two devices writing one
// SQLite file is not a supported mode. So the synced state is a small JSON
// snapshot next to the library, merged entry by entry.
//
// API keys are never written. The snapshot type below has no field that could
// carry one, so a key cannot leak even if a caller passes a full provider
// config: the conversion strips it by construction.
package settingsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
)

const (
	// FileName is the snapshot inside the shared folder. One file keeps the
	// materialization behaviour of iCloud simple: a single small document.
	FileName = "model-settings.json"
	// Version guards the merge format. Unknown newer versions are ignored
	// rather than guessed at, so an old build cannot mangle a new snapshot.
	Version = 1
)

// ProviderEntry is one provider as shared with other devices.
type ProviderEntry struct {
	ID                     string                 `json:"id"`
	Provider               domain.LLMProviderType `json:"provider"`
	Name                   string                 `json:"name"`
	BaseURL                string                 `json:"baseUrl,omitempty"`
	DefaultProtocolDialect string                 `json:"defaultProtocolDialect,omitempty"`
	Models                 []domain.LLMModelRef   `json:"models,omitempty"`
	CreatedAt              string                 `json:"createdAt,omitempty"`
	UpdatedAt              string                 `json:"updatedAt"`
}

// Tombstone records a deletion so it survives a device that was offline when
// the provider was removed. Without it, the next push from that device would
// resurrect the provider.
type Tombstone struct {
	ID        string `json:"id"`
	DeletedAt string `json:"deletedAt"`
}

// Snapshot is the shared document. It contains no API key by design.
type Snapshot struct {
	Version           int                      `json:"version"`
	Providers         []ProviderEntry          `json:"providers"`
	DeletedProviders  []Tombstone              `json:"deletedProviders,omitempty"`
	ModelAssignments  []domain.ModelAssignment `json:"modelAssignments,omitempty"`
	DefaultModels     []domain.ModelConfig     `json:"defaultModels,omitempty"`
	Review            config.ReviewSettings    `json:"review"`
	SettingsUpdatedAt string                   `json:"settingsUpdatedAt,omitempty"`
	WrittenAt         string                   `json:"writtenAt,omitempty"`
}

// Status is what the settings screen shows. It carries paths, never secrets.
type Status struct {
	Enabled      bool   `json:"enabled"`
	SyncDir      string `json:"syncDir,omitempty"`
	LastPulledAt string `json:"lastPulledAt,omitempty"`
	LastPushedAt string `json:"lastPushedAt,omitempty"`
}

type Service struct {
	dir      string
	llm      *llmconfig.Manager
	settings *config.Manager

	mu           sync.Mutex
	deleted      map[string]string // provider id → deletedAt
	lastPulledAt string
	lastPushedAt string
}

func New(dir string, llm *llmconfig.Manager, settings *config.Manager) *Service {
	return &Service{dir: dir, llm: llm, settings: settings, deleted: map[string]string{}}
}

func (s *Service) Enabled() bool { return s != nil && s.dir != "" && s.llm != nil && s.settings != nil }

func (s *Service) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *Service) Status() Status {
	if s == nil {
		return Status{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Enabled:      s.Enabled(),
		SyncDir:      s.dir,
		LastPulledAt: s.lastPulledAt,
		LastPushedAt: s.lastPushedAt,
	}
}

// MarkDeleted remembers a removal until the next successful push.
func (s *Service) MarkDeleted(id string) {
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	s.deleted[id] = now()
	s.mu.Unlock()
}

// Pull merges the shared snapshot into this device.
//
// Providers merge by UpdatedAt; a tombstone deletes a local provider only when
// it is at least as new as the local record. The settings block is taken whole
// from whichever side has the newer SettingsUpdatedAt — a per-field merge would
// need field-level clocks that do not exist.
func (s *Service) Pull(ctx context.Context) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	remote, err := s.read()
	if errors.Is(err, os.ErrNotExist) {
		// Nothing to merge yet, but the settings screen still wants to say when
		// this device last checked.
		s.mu.Lock()
		s.lastPulledAt = now()
		s.mu.Unlock()
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if remote.Version > Version {
		return false, fmt.Errorf("shared settings version %d is newer than this app supports (%d)", remote.Version, Version)
	}

	local, err := s.llm.GetAll(ctx)
	if err != nil {
		return false, err
	}
	localByID := make(map[string]domain.LLMProviderConfig, len(local))
	for _, cfg := range local {
		localByID[cfg.ID] = cfg
	}

	changed := false
	removed := map[string]bool{}
	for _, tomb := range remote.DeletedProviders {
		cfg, ok := localByID[tomb.ID]
		if !ok {
			continue
		}
		if newer(tomb.DeletedAt, cfg.UpdatedAt) || tomb.DeletedAt == cfg.UpdatedAt {
			if err := s.llm.Delete(ctx, cfg.ID); err != nil {
				return changed, err
			}
			delete(localByID, cfg.ID)
			removed[cfg.ID] = true
			changed = true
		}
	}

	remoteCfgs := make([]domain.LLMProviderConfig, 0, len(remote.Providers))
	for _, entry := range remote.Providers {
		if entry.ID == "" || removed[entry.ID] {
			continue
		}
		if current, ok := localByID[entry.ID]; ok && !newer(entry.UpdatedAt, current.UpdatedAt) {
			continue
		}
		remoteCfgs = append(remoteCfgs, entry.config())
	}
	if len(remoteCfgs) > 0 {
		if err := s.llm.ApplyRemote(ctx, remoteCfgs); err != nil {
			return changed, err
		}
		changed = true
	}

	localSettings := s.settings.Get()
	if newer(remote.SettingsUpdatedAt, localSettings.UpdatedAt) {
		if err := s.settings.ApplyRemote(remote.ModelAssignments, remote.DefaultModels, remote.Review, remote.SettingsUpdatedAt); err != nil {
			return changed, err
		}
		changed = true
	}

	s.mu.Lock()
	s.lastPulledAt = now()
	s.mu.Unlock()
	return changed, nil
}

// Push writes this device's state as the shared snapshot.
//
// Tombstones from the shared file are carried forward so a push from a device
// that has not seen another device's deletion cannot resurrect that provider.
func (s *Service) Push(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	providers, err := s.llm.GetAll(ctx)
	if err != nil {
		return err
	}
	settings := s.settings.Get()
	snap := Snapshot{
		Version:           Version,
		Providers:         make([]ProviderEntry, 0, len(providers)),
		ModelAssignments:  settings.ModelAssignments,
		DefaultModels:     settings.DefaultModels,
		Review:            settings.Review,
		SettingsUpdatedAt: settings.UpdatedAt,
		WrittenAt:         now(),
	}
	for _, cfg := range providers {
		snap.Providers = append(snap.Providers, ProviderEntry{
			ID:                     cfg.ID,
			Provider:               cfg.Provider,
			Name:                   cfg.Name,
			BaseURL:                cfg.BaseURL,
			DefaultProtocolDialect: cfg.DefaultProtocolDialect,
			Models:                 cfg.Models,
			CreatedAt:              cfg.CreatedAt,
			UpdatedAt:              cfg.UpdatedAt,
		})
	}
	sort.Slice(snap.Providers, func(i, j int) bool { return snap.Providers[i].ID < snap.Providers[j].ID })

	s.mu.Lock()
	tombstones := make(map[string]string, len(s.deleted))
	for id, at := range s.deleted {
		tombstones[id] = at
	}
	s.mu.Unlock()
	if remote, err := s.read(); err == nil {
		for _, tomb := range remote.DeletedProviders {
			if !newer(tombstones[tomb.ID], tomb.DeletedAt) {
				tombstones[tomb.ID] = tomb.DeletedAt
			}
		}
	}
	live := make(map[string]bool, len(snap.Providers))
	for _, entry := range snap.Providers {
		live[entry.ID] = true
	}
	for id, at := range tombstones {
		// A provider that exists here again was re-created, so the tombstone is
		// spent; keeping it would delete the provider on the next pull.
		if live[id] {
			delete(tombstones, id)
			continue
		}
		snap.DeletedProviders = append(snap.DeletedProviders, Tombstone{ID: id, DeletedAt: at})
	}
	sort.Slice(snap.DeletedProviders, func(i, j int) bool {
		return snap.DeletedProviders[i].ID < snap.DeletedProviders[j].ID
	})

	if err := s.write(snap); err != nil {
		return err
	}
	s.mu.Lock()
	s.deleted = map[string]string{}
	s.lastPushedAt = now()
	s.mu.Unlock()
	return nil
}

// Sync pulls then pushes. It is what the "sync now" button runs.
func (s *Service) Sync(ctx context.Context) (bool, error) {
	changed, err := s.Pull(ctx)
	if err != nil {
		return changed, err
	}
	return changed, s.Push(ctx)
}

func (s *Service) path() string { return filepath.Join(s.dir, FileName) }

func (s *Service) read() (Snapshot, error) {
	var snap Snapshot
	raw, err := os.ReadFile(s.path())
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return snap, fmt.Errorf("decode %s: %w", FileName, err)
	}
	return snap, nil
}

// write is a temp-file rename so a half-written snapshot is never observed by
// the other device. The file stays 0600 even though it holds no keys: it still
// describes the author's setup.
func (s *Service) write(snap Snapshot) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, FileName+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path())
}

func (e ProviderEntry) config() domain.LLMProviderConfig {
	return domain.LLMProviderConfig{
		ID:                     e.ID,
		Provider:               e.Provider,
		Name:                   e.Name,
		BaseURL:                e.BaseURL,
		DefaultProtocolDialect: e.DefaultProtocolDialect,
		Models:                 e.Models,
		CreatedAt:              e.CreatedAt,
		UpdatedAt:              e.UpdatedAt,
	}
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

// newer reports whether a is strictly later than b. Empty or malformed values
// sort oldest, so a record without a clock never wins against a stamped one.
func newer(a, b string) bool { return parseTime(a).After(parseTime(b)) }
