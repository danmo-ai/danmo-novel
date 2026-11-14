package runtimepaths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// containerSuffix identifies the app's iCloud container directory.
//
// An iCloud container is not stored under `com~apple~CloudDocs` (that is the
// user's own iCloud Drive). Each container is a sibling directory named after
// the container identifier, so `iCloud.com.danmo.novel` materializes as
// `iCloud~com~danmo~novel`; older Xcode builds prefixed the team id
// (`<TEAMID>~iCloud~com~danmo~novel`). Matching the suffix accepts both without
// a second branch.
const containerSuffix = "~com~danmo~novel"

// iCloudDocumentsDir is the shared Documents folder on macOS, or "" when this
// process must stay local.
func iCloudDocumentsDir() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	// Tests, the embedded core and any host that called SetHome with a scratch
	// directory run against a temporary home. They must never reach into the
	// developer's real iCloud library, so sharing is tied to owning the user's
	// actual workspace.
	if !ownsUserWorkspace() {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	mobile := filepath.Join(home, "Library", "Mobile Documents")
	entries, err := os.ReadDir(mobile)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.Contains(name, "icloud") || !strings.HasSuffix(name, containerSuffix) {
			continue
		}
		// Only a materialized container counts. The desktop app has no iCloud
		// entitlement and cannot create one; a directory it made itself would
		// never sync, so a missing Documents folder means "not available".
		documents := filepath.Join(mobile, entry.Name(), "Documents")
		if info, err := os.Stat(documents); err == nil && info.IsDir() {
			return documents
		}
	}
	return ""
}

// ownsUserWorkspace reports whether Home() is the platform default location.
func ownsUserWorkspace() bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	return filepath.Clean(Home()) == filepath.Join(home, ".danmo-novel")
}
