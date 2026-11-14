package settingsync_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/service/settingsync"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

type fixture struct {
	dir      string
	settings *config.Manager
	llm      *llmconfig.Manager
	svc      *settingsync.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(home, "novel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	llm := llmconfig.NewManager(db, llmconfig.NewModelRegistry())
	dir := t.TempDir()
	return &fixture{dir: dir, settings: settings, llm: llm, svc: settingsync.New(dir, llm, settings)}
}

func (f *fixture) addProvider(t *testing.T, name, key string) domain.LLMProviderConfig {
	t.Helper()
	cfg, err := f.llm.Upsert(context.Background(), domain.UpsertLLMProviderConfigRequest{
		Provider: domain.LLMProviderOpenAI,
		Name:     name,
		APIKey:   key,
		BaseURL:  "https://api.openai.com/v1",
		Models:   []domain.LLMModelRef{{Name: "gpt-4.1-mini", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func (f *fixture) writeRemote(t *testing.T, snap settingsync.Snapshot) {
	t.Helper()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, settingsync.FileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) readRemote(t *testing.T) settingsync.Snapshot {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, settingsync.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var snap settingsync.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func later(minutes int) string {
	return time.Now().UTC().Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
}

// The snapshot must never carry a key, whatever the provider row holds.
func TestPushWritesProvidersWithoutAPIKey(t *testing.T) {
	f := newFixture(t)
	cfg := f.addProvider(t, "openai", "sk-very-secret")
	if err := f.svc.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(f.dir, settingsync.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-very-secret") {
		t.Fatalf("snapshot leaked the API key:\n%s", raw)
	}
	if strings.Contains(string(raw), "apiKey") {
		t.Fatalf("snapshot has an apiKey field:\n%s", raw)
	}
	snap := f.readRemote(t)
	if len(snap.Providers) != 1 || snap.Providers[0].ID != cfg.ID {
		t.Fatalf("providers=%+v want just %s", snap.Providers, cfg.ID)
	}
	info, err := os.Stat(filepath.Join(f.dir, settingsync.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("snapshot mode=%o want 600", mode)
	}
}

// A remote edit wins on UpdatedAt, but the receiving device keeps its own key.
func TestPullAppliesNewerProviderAndKeepsLocalKey(t *testing.T) {
	f := newFixture(t)
	local := f.addProvider(t, "openai", "local-key")
	f.writeRemote(t, settingsync.Snapshot{
		Version: settingsync.Version,
		Providers: []settingsync.ProviderEntry{{
			ID:        local.ID,
			Provider:  domain.LLMProviderOpenAI,
			Name:      "openai",
			BaseURL:   "https://proxy.example/v1",
			Models:    []domain.LLMModelRef{{Name: "gpt-5", Enabled: true}},
			UpdatedAt: later(30),
		}},
		Review: config.ReviewSettings{Concurrency: 3, Rounds: 3},
	})
	if _, err := f.svc.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := f.llm.GetByID(context.Background(), local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "https://proxy.example/v1" {
		t.Fatalf("baseUrl=%q want the remote value", got.BaseURL)
	}
	if len(got.Models) != 1 || got.Models[0].Name != "gpt-5" {
		t.Fatalf("models=%+v want the remote list", got.Models)
	}
	if got.APIKey != "local-key" {
		t.Fatalf("apiKey=%q want the local key preserved", got.APIKey)
	}
}

// An older remote copy must not roll back a newer local edit.
func TestPullKeepsNewerLocalProvider(t *testing.T) {
	f := newFixture(t)
	local := f.addProvider(t, "openai", "local-key")
	f.writeRemote(t, settingsync.Snapshot{
		Version: settingsync.Version,
		Providers: []settingsync.ProviderEntry{{
			ID:        local.ID,
			Provider:  domain.LLMProviderOpenAI,
			Name:      "openai",
			BaseURL:   "https://stale.example/v1",
			UpdatedAt: "2000-01-01T00:00:00Z",
		}},
		Review: config.ReviewSettings{Concurrency: 3, Rounds: 3},
	})
	if _, err := f.svc.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := f.llm.GetByID(context.Background(), local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("baseUrl=%q want the local value", got.BaseURL)
	}
}

// A deletion on another device removes the provider here when the tombstone is
// at least as new as the local row.
func TestPullDeletesProviderWithNewerTombstone(t *testing.T) {
	f := newFixture(t)
	local := f.addProvider(t, "openai", "local-key")
	f.writeRemote(t, settingsync.Snapshot{
		Version:          settingsync.Version,
		DeletedProviders: []settingsync.Tombstone{{ID: local.ID, DeletedAt: later(30)}},
		// The stale provider entry in the same file must not resurrect it.
		Providers: []settingsync.ProviderEntry{{
			ID: local.ID, Provider: domain.LLMProviderOpenAI, Name: "openai", UpdatedAt: later(30),
		}},
		Review: config.ReviewSettings{Concurrency: 3, Rounds: 3},
	})
	if _, err := f.svc.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := f.llm.GetByID(context.Background(), local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "" {
		t.Fatalf("provider %s survived a newer tombstone", local.ID)
	}
}

// Push must write the local deletion as a tombstone, or the other device would
// re-add the provider on the next pull.
func TestPushCarriesLocalDeletionAsTombstone(t *testing.T) {
	f := newFixture(t)
	local := f.addProvider(t, "openai", "local-key")
	if err := f.llm.Delete(context.Background(), local.ID); err != nil {
		t.Fatal(err)
	}
	f.svc.MarkDeleted(local.ID)
	if err := f.svc.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap := f.readRemote(t)
	if len(snap.DeletedProviders) != 1 || snap.DeletedProviders[0].ID != local.ID {
		t.Fatalf("tombstones=%+v want %s", snap.DeletedProviders, local.ID)
	}
	if len(snap.Providers) != 0 {
		t.Fatalf("providers=%+v want none", snap.Providers)
	}
}

// A provider that exists again spends its tombstone instead of being deleted
// by the next pull.
func TestPushDropsTombstoneForRecreatedProvider(t *testing.T) {
	f := newFixture(t)
	local := f.addProvider(t, "openai", "local-key")
	f.svc.MarkDeleted(local.ID)
	if err := f.svc.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snap := f.readRemote(t); len(snap.DeletedProviders) != 0 {
		t.Fatalf("tombstones=%+v want none for a live provider", snap.DeletedProviders)
	}
}

// The settings block merges as one unit on its own clock.
func TestPullAppliesNewerSettingsBlock(t *testing.T) {
	f := newFixture(t)
	f.writeRemote(t, settingsync.Snapshot{
		Version:           settingsync.Version,
		ModelAssignments:  []domain.ModelAssignment{{Role: domain.StageWrite, ModelID: "openai/gpt-5"}},
		DefaultModels:     []domain.ModelConfig{{Model: "gpt-5", ContextWindow: 256000}},
		Review:            config.ReviewSettings{Concurrency: 6, Strictness: "strict", Rounds: 2},
		SettingsUpdatedAt: later(30),
	})
	if _, err := f.svc.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := f.settings.Get()
	if got.Review.Rounds != 2 || got.Review.Concurrency != 6 {
		t.Fatalf("review=%+v want the remote values", got.Review)
	}
	if len(got.ModelAssignments) != 1 || got.ModelAssignments[0].ModelID != "openai/gpt-5" {
		t.Fatalf("assignments=%+v want the remote value", got.ModelAssignments)
	}
	if got.UpdatedAt == "" {
		t.Fatal("applying a remote block must keep its timestamp")
	}
}

// A local settings edit is newer, so a stale remote file must not undo it.
func TestPullKeepsNewerLocalSettings(t *testing.T) {
	f := newFixture(t)
	if err := f.settings.Update(func(s *config.Settings) {
		s.Review.Rounds = 4
	}); err != nil {
		t.Fatal(err)
	}
	f.writeRemote(t, settingsync.Snapshot{
		Version:           settingsync.Version,
		Review:            config.ReviewSettings{Concurrency: 3, Rounds: 1},
		SettingsUpdatedAt: "2000-01-01T00:00:00Z",
	})
	if _, err := f.svc.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.settings.Get().Review.Rounds; got != 4 {
		t.Fatalf("rounds=%d want the newer local value", got)
	}
}

func TestDisabledServiceIsANoop(t *testing.T) {
	f := newFixture(t)
	f.addProvider(t, "openai", "local-key")
	disabled := settingsync.New("", f.llm, f.settings)
	if disabled.Enabled() {
		t.Fatal("empty dir must not report enabled")
	}
	if _, err := disabled.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := disabled.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	disabled.MarkDeleted("llm-openai")
	if st := disabled.Status(); st.Enabled {
		t.Fatalf("status=%+v want disabled", st)
	}
}

// iCloud parks an undownloaded file as `<name>.icloud`. Reading the real name
// fails with ENOENT, which looks like "no snapshot yet"; treating that as absent
// would let this device push its own state over the other device's.
func TestPushRefusesToOverwriteUndownloadedPlaceholder(t *testing.T) {
	f := newFixture(t)
	// A snapshot this device cannot read, but which exists on the other side.
	raw := `{"version":1,"providers":[{"id":"llm-other","provider":"openai","name":"other","updatedAt":"2026-11-14T00:00:00Z"}],"review":{}}`
	placeholder := filepath.Join(f.dir, settingsync.FileName+".icloud")
	if err := os.WriteFile(placeholder, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	f.addProvider(t, "openai", "local-key")

	if err := f.svc.Push(context.Background()); err == nil {
		t.Fatal("push must refuse when the shared snapshot exists but cannot be read")
	}
	// The placeholder still holds the other device's data, untouched.
	got, err := os.ReadFile(placeholder)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != raw {
		t.Fatalf("placeholder was overwritten: %s", got)
	}
	if _, err := os.Stat(filepath.Join(f.dir, settingsync.FileName)); !os.IsNotExist(err) {
		t.Fatal("push created a snapshot next to the placeholder")
	}
}
