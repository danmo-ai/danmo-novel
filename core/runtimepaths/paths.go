package runtimepaths

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var homeOverride struct {
	sync.RWMutex
	path string
}

// SetHome binds one core instance to an explicit data directory. Call before
// opening settings or SQLite; the rest of the core uses the same path helpers.
func SetHome(home string) error {
	if home == "" {
		return fmt.Errorf("home path required")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	homeOverride.Lock()
	homeOverride.path = abs
	homeOverride.Unlock()
	return os.Setenv("NOVEL_HOME", abs)
}

func Home() string {
	homeOverride.RLock()
	path := homeOverride.path
	homeOverride.RUnlock()
	if path != "" {
		return path
	}
	if v := os.Getenv("NOVEL_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("WORK_HOME"); v != "" {
		return filepath.Join(filepath.Dir(v), ".danmo-novel")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".danmo-novel")
}

func DBPath() string {
	if v := os.Getenv("NOVEL_DB_PATH"); v != "" {
		return v
	}
	return filepath.Join(Home(), "novel.db")
}

// DefaultLibraryRoot is where books live when the author has not chosen a
// directory: macOS uses the app's iCloud container, other systems the local
// home. The choice is by system, not by host, so the packaged app, `make dev`
// and a bare `go run` on one Mac read the same books.
//
// NOVEL_LIBRARY is the one host override: iOS passes its ubiquity container,
// which lives inside the app sandbox and cannot be found here.
func DefaultLibraryRoot() string {
	if v := os.Getenv("NOVEL_LIBRARY"); v != "" {
		return v
	}
	if dir := iCloudDocumentsDir(); dir != "" {
		return filepath.Join(dir, "library")
	}
	return filepath.Join(Home(), "library")
}

func SettingsPath() string { return filepath.Join(Home(), "config", "settings.json") }

// SyncDir is the folder the model settings snapshot lives in: next to the books
// in iCloud on macOS, empty where there is no shared folder.
func SyncDir() string {
	if v := os.Getenv("NOVEL_SYNC_DIR"); v != "" {
		return v
	}
	return iCloudDocumentsDir()
}

func EnsureDirs() error {
	paths := []string{Home(), filepath.Join(Home(), "config"), DefaultLibraryRoot()}
	if dir := SyncDir(); dir != "" {
		paths = append(paths, dir)
	}
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}
