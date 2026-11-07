package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWebSPAFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<div id=app>ok</div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(&App{WebRoot: root})
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/books/demo", nil))
	if res.Code != 200 {
		t.Fatalf("SPA fallback: %d %s", res.Code, res.Body.String())
	}
}
