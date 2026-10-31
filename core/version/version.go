package version

import "runtime/debug"

// Version is injected at link time via -ldflags for release builds.
// When it is still the placeholder (local `go run` / plain `go build`), fall
// back to the VCS revision embedded by the Go toolchain so the UI can show a
// real identifier instead of "dev".
var Version = "dev"

func init() {
	if Version != "" && Version != "dev" {
		return
	}
	if v := vcsVersion(); v != "" {
		Version = v
	}
}

func vcsVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if modified == "true" {
		return rev + "-dirty"
	}
	return rev
}
