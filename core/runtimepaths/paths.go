package runtimepaths

import (
	"os"
	"path/filepath"
)

func Home() string {
	if v := os.Getenv("NOVEL_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("WORK_HOME"); v != "" {
		// allow sharing machine home layout during migration experiments
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

func SettingsPath() string {
	return filepath.Join(Home(), "config", "settings.json")
}

func EnsureDirs() error {
	for _, p := range []string{
		Home(),
		filepath.Join(Home(), "config"),
		LibraryRoot(),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}
