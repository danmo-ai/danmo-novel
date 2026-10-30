package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
	"github.com/danmo-ai/danmo-novel/core/service/runs"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

func TestWriteAPIPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	if err := WriteAPIPort("127.0.0.1:7804"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(runtimepaths.Home(), "api.port"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "7804" {
		t.Fatalf("api.port=%q", string(raw))
	}
}

func TestMCPStreamableHTTP_InitializeHealthListBooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock := llm.NewMock()
	runSvc := runs.New(db, settings, mock)
	srv := New(&Deps{Settings: settings, Store: db, Runs: runSvc})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	initBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	}
	resp := postMCP(t, ts, initBody, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("initialize status=%d body=%s", resp.StatusCode, b)
	}
	var initOut map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&initOut); err != nil {
		t.Fatal(err)
	}
	if initOut["error"] != nil {
		t.Fatalf("initialize error: %v", initOut["error"])
	}
	result, _ := initOut["result"].(map[string]any)
	if result == nil {
		t.Fatalf("no result: %v", initOut)
	}
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "danmo-novel" {
		t.Fatalf("serverInfo=%v", serverInfo)
	}

	health := callTool(t, ts, "health", map[string]any{})
	if !strings.Contains(health, `"ok": true`) && !strings.Contains(health, `"ok":true`) {
		t.Fatalf("health=%s", health)
	}
	if !strings.Contains(health, "danmo-novel") {
		t.Fatalf("health missing app: %s", health)
	}

	books := callTool(t, ts, "list_books", map[string]any{})
	if strings.TrimSpace(books) != "[]" && !strings.Contains(books, "[") {
		t.Fatalf("list_books=%s", books)
	}
}

func postMCP(t *testing.T, ts *httptest.Server, body any, sessionID string) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func callTool(t *testing.T, ts *httptest.Server, name string, args map[string]any) string {
	t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	resp := postMCP(t, ts, body, "")
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/call %s status=%d body=%s", name, resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		// streamable may return SSE; fall back to raw contains
		if strings.Contains(string(raw), name) || strings.Contains(string(raw), "text") {
			return string(raw)
		}
		t.Fatalf("decode: %v body=%s", err, raw)
	}
	if out["error"] != nil {
		t.Fatalf("tool %s error: %v", name, out["error"])
	}
	result, _ := out["result"].(map[string]any)
	contents, _ := result["content"].([]any)
	var texts []string
	for _, c := range contents {
		m, _ := c.(map[string]any)
		if m["type"] == "text" {
			texts = append(texts, m["text"].(string))
		}
	}
	joined := strings.Join(texts, "\n")
	if result["isError"] == true {
		t.Fatalf("tool %s isError: %s", name, joined)
	}
	return joined
}
