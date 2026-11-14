package runtimepaths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func requireDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("iCloud containers only exist on macOS")
	}
}

// fakeUserHome points HOME at a scratch directory and leaves NOVEL_HOME unset,
// so Home() is that user's default workspace.
func fakeUserHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NOVEL_HOME", "")
	t.Setenv("NOVEL_LIBRARY", "")
	t.Setenv("NOVEL_SYNC_DIR", "")
	return home
}

func makeContainer(t *testing.T, home, name string) string {
	t.Helper()
	documents := filepath.Join(home, "Library", "Mobile Documents", name, "Documents")
	if err := os.MkdirAll(documents, 0o755); err != nil {
		t.Fatal(err)
	}
	return documents
}

// macOS always books through iCloud; there is no switch and no host flag.
func TestMacOSUsesICloudLibrary(t *testing.T) {
	requireDarwin(t)
	home := fakeUserHome(t)
	documents := makeContainer(t, home, "iCloud~com~danmo~novel")

	if got := SyncDir(); got != documents {
		t.Fatalf("SyncDir=%q want %q", got, documents)
	}
	if got, want := DefaultLibraryRoot(), filepath.Join(documents, "library"); got != want {
		t.Fatalf("LibraryRoot=%q want %q", got, want)
	}
}

// Older Xcode builds prefixed the team id; the same container must be found.
func TestMacOSAcceptsTeamPrefixedContainer(t *testing.T) {
	requireDarwin(t)
	home := fakeUserHome(t)
	documents := makeContainer(t, home, "TEAMID123~iCloud~com~danmo~novel")
	if got := SyncDir(); got != documents {
		t.Fatalf("SyncDir=%q want %q", got, documents)
	}
}

// A Mac that has never run the iOS app has no container. Writing into a
// directory iCloud does not know about would silently never sync, so the local
// home stays in use until the container exists.
func TestMacOSWithoutContainerStaysLocal(t *testing.T) {
	requireDarwin(t)
	home := fakeUserHome(t)
	if got := SyncDir(); got != "" {
		t.Fatalf("SyncDir=%q want empty without a container", got)
	}
	if got, want := DefaultLibraryRoot(), filepath.Join(home, ".danmo-novel", "library"); got != want {
		t.Fatalf("LibraryRoot=%q want %q", got, want)
	}
}

// A scratch home is an isolated workspace: tests, the embedded core and
// anything else that calls SetHome never touch the user's iCloud library, even
// on a Mac where the container exists.
func TestIsolatedHomeNeverUsesICloud(t *testing.T) {
	requireDarwin(t)
	home := fakeUserHome(t)
	makeContainer(t, home, "iCloud~com~danmo~novel")
	scratch := t.TempDir()
	t.Setenv("NOVEL_HOME", scratch)

	if got := SyncDir(); got != "" {
		t.Fatalf("SyncDir=%q want empty for an isolated home", got)
	}
	if got, want := DefaultLibraryRoot(), filepath.Join(scratch, "library"); got != want {
		t.Fatalf("LibraryRoot=%q want %q", got, want)
	}
}

// iOS passes its ubiquity container explicitly because it lives in the app
// sandbox; that path must win over everything else.
func TestExplicitLibraryWins(t *testing.T) {
	requireDarwin(t)
	home := fakeUserHome(t)
	makeContainer(t, home, "iCloud~com~danmo~novel")
	library := filepath.Join(t.TempDir(), "container-library")
	syncDir := filepath.Join(t.TempDir(), "container-sync")
	t.Setenv("NOVEL_LIBRARY", library)
	t.Setenv("NOVEL_SYNC_DIR", syncDir)

	if got := DefaultLibraryRoot(); got != library {
		t.Fatalf("LibraryRoot=%q want %q", got, library)
	}
	if got := SyncDir(); got != syncDir {
		t.Fatalf("SyncDir=%q want %q", got, syncDir)
	}
}
