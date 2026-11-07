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

func LibraryRoot() string {
	if v := os.Getenv("NOVEL_LIBRARY"); v != "" {
		return v
	}
	return filepath.Join(Home(), "library")
}

func SettingsPath() string { return filepath.Join(Home(), "config", "settings.json") }

func EnsureDirs() error {
	for _, p := range []string{Home(), filepath.Join(Home(), "config"), LibraryRoot()} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}
