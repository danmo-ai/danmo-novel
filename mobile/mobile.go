// Package danmonovel exposes the local core to gomobile. Only scalar types are
// exported so the same binding works from Swift and Kotlin.
package danmonovel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/serve"
)

var current struct {
	sync.Mutex
	core *serve.Server
	home string
}

type result struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Home  string `json:"home,omitempty"`
}

func reply(err error) string {
	if err == nil {
		return `{"ok":true}`
	}
	raw, _ := json.Marshal(result{Error: err.Error()})
	return string(raw)
}

// SetPaths hands the core the shared workspace the host resolved. Call it
// before Start.
//
// It exists because the host cannot do this itself: Go snapshots the process
// environment when its runtime starts, so a libc setenv from Swift or Kotlin
// afterwards is invisible to os.Getenv. The iOS app resolves its ubiquity
// container — reachable only through FileManager — and this call is the one way
// to tell the core about it.
func SetPaths(libraryRoot, syncDir string) string {
	if libraryRoot != "" {
		if err := os.Setenv("NOVEL_LIBRARY", libraryRoot); err != nil {
			return reply(err)
		}
	}
	if syncDir != "" {
		if err := os.Setenv("NOVEL_SYNC_DIR", syncDir); err != nil {
			return reply(err)
		}
	}
	return `{"ok":true}`
}

// Start prepares the local database, library and API handler. No TCP listener
// is opened on mobile; API calls go through Request. Re-starting an existing
// core is idempotent and reports its actual home so a host ViewModel cannot
// mistake a previous sandbox for its own.
func Start(home string) string {
	current.Lock()
	defer current.Unlock()
	if current.core != nil && current.home == home {
		raw, _ := json.Marshal(result{OK: true, Home: current.home})
		return string(raw)
	}
	if current.core != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := current.core.Stop(ctx)
		cancel()
		if err != nil {
			return reply(err)
		}
		current.core = nil
		current.home = ""
	}
	core, err := serve.New(serve.Options{Home: home, DynamicPort: true})
	if err != nil {
		return reply(err)
	}
	current.core = core
	current.home = home
	raw, _ := json.Marshal(result{OK: true, Home: home})
	return string(raw)
}

// Request runs one HTTP-contract request in-process. The response is a JSON
// envelope containing numeric status and the original response body string.
func Request(method, path, body, authorization string) string {
	current.Lock()
	core := current.core
	current.Unlock()
	if core == nil {
		return reply(errors.New("local core is not started"))
	}
	return core.Request(method, path, body, authorization)
}

func Stop() string {
	current.Lock()
	core := current.core
	current.core = nil
	current.home = ""
	current.Unlock()
	if core == nil {
		return `{"ok":true}`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return reply(core.Stop(ctx))
}
