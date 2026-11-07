// Package danmonovel exposes the local core to gomobile. Only scalar types are
// exported so the same binding works from Swift and Kotlin.
package danmonovel

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/serve"
)

var current struct {
	sync.Mutex
	core *serve.Server
}

type result struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func reply(err error) string {
	if err == nil {
		return `{"ok":true}`
	}
	raw, _ := json.Marshal(result{Error: err.Error()})
	return string(raw)
}

// Start prepares the local database, library and API handler. No TCP listener
// is opened on mobile; API calls go through Request.
func Start(home string) string {
	current.Lock()
	defer current.Unlock()
	if current.core != nil {
		return `{"ok":true}`
	}
	core, err := serve.New(serve.Options{Home: home, DynamicPort: true})
	if err != nil {
		return reply(err)
	}
	current.core = core
	return `{"ok":true}`
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
	current.Unlock()
	if core == nil {
		return `{"ok":true}`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return reply(core.Stop(ctx))
}
