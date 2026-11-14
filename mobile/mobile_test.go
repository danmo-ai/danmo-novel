package danmonovel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInProcessCoreAndSQLiteRoundTrip(t *testing.T) {
	home := t.TempDir()
	if got := Start(home); !strings.Contains(got, `"ok":true`) || !strings.Contains(got, `"home":`) {
		t.Fatalf("start: %s", got)
	}
	defer Stop()
	response := Request("GET", "/api/system/health", "", "")
	var envelope struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(response), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != 200 {
		t.Fatalf("health: %s", response)
	}
	var health map[string]any
	if err := json.Unmarshal([]byte(envelope.Body), &health); err != nil || health["ok"] != true {
		t.Fatalf("health body: %s (%v)", envelope.Body, err)
	}
	if _, err := os.Stat(filepath.Join(home, "novel.db-wal")); err != nil {
		t.Fatalf("WAL was not enabled: %v", err)
	}
	if got := Stop(); got != `{"ok":true}` {
		t.Fatalf("stop: %s", got)
	}
	if got := Start(filepath.Join(home, "restart")); !strings.Contains(got, `"ok":true`) {
		t.Fatalf("restart: %s", got)
	}
}

func TestStartIsIdempotentAndReportsActualHome(t *testing.T) {
	home := t.TempDir()
	if got := Start(home); !strings.Contains(got, `"home":`) {
		t.Fatalf("start: %s", got)
	}
	defer Stop()
	response := Start(home)
	var started result
	if err := json.Unmarshal([]byte(response), &started); err != nil {
		t.Fatal(err)
	}
	if !started.OK || started.Home != home {
		t.Fatalf("reused core reported wrong home: %s", response)
	}
}

func TestStartDifferentHomeDoesNotReuseLibrary(t *testing.T) {
	first := t.TempDir()
	if got := Start(first); !strings.Contains(got, `"ok":true`) {
		t.Fatal(got)
	}
	defer Stop()
	if got := Request("POST", "/api/books/demo", "{}", ""); !strings.Contains(got, `"status":200`) {
		t.Fatal(got)
	}
	second := t.TempDir()
	if got := Start(second); !strings.Contains(got, `"home":`) {
		t.Fatal(got)
	}
	response := Request("GET", "/api/books", "", "")
	if !strings.Contains(response, `"body":"[]"`) {
		t.Fatalf("second home inherited first library: %s", response)
	}
}
