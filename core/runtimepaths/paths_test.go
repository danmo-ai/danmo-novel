package runtimepaths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirIsEmptyWithoutSharedFolder(t *testing.T) {
	// A scratch home means no iCloud container is in play on any machine.
	t.Setenv("NOVEL_HOME", t.TempDir())
	t.Setenv("NOVEL_SYNC_DIR", "")
	if got := SyncDir(); got != "" {
		t.Fatalf("SyncDir=%q want empty when nothing shared is configured", got)
	}
}

func TestSyncDirUsesInjectedFolder(t *testing.T) {
	t.Setenv("NOVEL_HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("NOVEL_SYNC_DIR", dir)
	if got := SyncDir(); got != dir {
		t.Fatalf("SyncDir=%q want %q", got, dir)
	}
}

// The core starts by calling EnsureDirs; the shared folder must be created in
// the same pass, otherwise the first settings push has nowhere to write.
func TestEnsureDirsCreatesSharedFolder(t *testing.T) {
	home := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	syncDir := filepath.Join(t.TempDir(), "cloud")
	t.Setenv("NOVEL_HOME", home)
	t.Setenv("NOVEL_LIBRARY", library)
	t.Setenv("NOVEL_SYNC_DIR", syncDir)

	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{home, filepath.Join(home, "config"), library, syncDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("%s was not created: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}
}

func TestEnsureDirsSkipsSharedFolderWhenUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	t.Setenv("NOVEL_LIBRARY", filepath.Join(home, "library"))
	t.Setenv("NOVEL_SYNC_DIR", "")
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
}
