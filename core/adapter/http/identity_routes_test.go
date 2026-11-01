package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

func TestIdentitySegmentRouteLifecycle(t *testing.T) {
	root, err := books.CreateBookScaffold(t.TempDir(), "identity-http", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snap, _ := books.LoadBook(root, false)
	if err := store.UpsertBook(context.Background(), "identity-http", root, "测试", snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(&App{Store: store})
	body := map[string]any{"action": "append", "segment": domain.IdentitySegment{Stem: "lin", RoleTitle: "内勤新警", Org: "派出所", FromUnit: "v01-U1"}, "baseRevision": snap.Revision}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/books/identity-http/identity-segments", bytes.NewReader(raw))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("create identity: %d %s", res.Code, res.Body.String())
	}
	segments, err := books.LoadIdentityTimeline(root)
	if err != nil || len(segments) != 1 {
		t.Fatalf("identity not persisted: %+v %v", segments, err)
	}
	// Same revision and duplicate identity must not be accepted again.
	req = httptest.NewRequest(http.MethodPost, "/api/books/identity-http/identity-segments", bytes.NewReader(raw))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("stale revision must be rejected before duplicate validation: %d %s", res.Code, res.Body.String())
	}
	// A duplicate with the latest revision is a validation error, not a new segment.
	snap, _ = books.LoadBook(root, false)
	body["baseRevision"] = snap.Revision
	raw, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/api/books/identity-http/identity-segments", bytes.NewReader(raw))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate identity: %d %s", res.Code, res.Body.String())
	}
	// A stale revision is a conflict, not an implicit overwrite.
	body["baseRevision"] = "stale-revision"
	raw, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/api/books/identity-http/identity-segments", bytes.NewReader(raw))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("stale revision: %d %s", res.Code, res.Body.String())
	}
	// Generic file editing must not bypass identity validation.
	fileBody, _ := json.Marshal(map[string]string{"path": books.IdentityTimelinePath, "content": "segments: []"})
	req = httptest.NewRequest(http.MethodPut, "/api/books/identity-http/file", bytes.NewReader(fileBody))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("generic identity edit must be rejected: %d %s", res.Code, res.Body.String())
	}
}
