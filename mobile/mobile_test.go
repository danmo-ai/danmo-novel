package danmonovel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInProcessCoreAndSQLiteRoundTrip(t *testing.T) {
	home := t.TempDir()
	if got := Start(home); got != `{"ok":true}` {
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
	if got := Start(filepath.Join(home, "restart")); got != `{"ok":true}` {
		t.Fatalf("restart: %s", got)
	}
}
