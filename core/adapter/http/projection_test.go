package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// The progressive-load contract: projections shrink the payload, keep what
// each view renders, and leave the default (parameter-less) responses intact
// so older clients — Android included — keep working.
func projectionFixture(t *testing.T) (*httptest.Server, *sqlite.Store, string) {
	t.Helper()
	root, _, err := books.CreateDemoBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	bookID := snap.Summary.ID
	if err := store.UpsertBook(context.Background(), bookID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	run := domain.Run{
		ID: "run-1", BookID: bookID, Kind: domain.TaskWrite, Status: domain.RunSucceeded,
		Spec:   domain.TaskSpec{Kind: domain.TaskWrite, BookID: bookID, UnitID: "v01-U1", BaseRevision: snap.Revision},
		Policy: domain.CallPolicy{MaxCalls: 3}, ModelID: "mock/model", CallsUsed: 2,
		Calls: []domain.CallRecord{
			{Index: 1, ModelID: "mock/model", Step: "chapter", PromptTokens: 1000, CompletionTokens: 500, CacheReadTokens: 800, DurationMs: 1200, StartedAt: now},
			{Index: 2, ModelID: "mock/model", Step: "repair", PromptTokens: 1100, CompletionTokens: 50, CacheReadTokens: 1050, CacheCreationTokens: 40, DurationMs: 300, StartedAt: now},
		},
		ContextSummary: []string{"单元合同", "连续性"},
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := store.SaveRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		ID: "cand-1", RunID: "run-1", BookID: bookID, Kind: domain.TaskWrite, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: "v01-U1",
		Chapters:     []domain.ChapterDraft{{Chapter: 1, Title: "雾起", Body: strings.Repeat("正文。", 400)}},
		BaseChapters: []domain.ChapterDraft{{Chapter: 1, Title: "雾起", Body: strings.Repeat("旧稿。", 400)}},
		CreatedAt:    now,
	}
	if err := store.SaveCandidate(context.Background(), cand); err != nil {
		t.Fatal(err)
	}
	review := domain.Candidate{
		ID: "cand-review", RunID: "run-1", BookID: bookID, Kind: domain.TaskReview, Status: domain.CandidateOpen,
		BaseRevision: snap.Revision, UnitID: "v01-U1",
		Findings:  []domain.ReviewFinding{{ID: "f1", Severity: "blocking", Source: "ai_review", Title: "时间不一致", Chapter: 1, RoundsHit: 2}},
		CreatedAt: now,
	}
	if err := store.SaveCandidate(context.Background(), review); err != nil {
		t.Fatal(err)
	}
	// A closed review list for the history endpoint.
	archived := review
	archived.ID = "cand-archived"
	archived.Status = domain.CandidateAccepted
	archived.Chapters = []domain.ChapterDraft{{Chapter: 1, Title: "雾起", Body: strings.Repeat("被审正文。", 400)}}
	if err := store.SaveCandidate(context.Background(), archived); err != nil {
		t.Fatal(err)
	}

	router := NewRouter(&App{Store: store})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, store, bookID
}

func getJSON(t *testing.T, server *httptest.Server, path string) ([]byte, map[string]any) {
	t.Helper()
	res, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", path, res.StatusCode)
	}
	var body map[string]any
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("GET %s: invalid JSON: %v", path, err)
	}
	return data, body
}

func TestBookProjections(t *testing.T) {
	server, _, bookID := projectionFixture(t)

	// Slim-only: the default response IS the navigation skeleton.
	skeletonRaw, skeleton := getJSON(t, server, "/api/books/"+bookID)
	filesRaw, files := getJSON(t, server, "/api/books/"+bookID+"?include=files")

	bookOf := func(body map[string]any) map[string]any { return body["book"].(map[string]any) }

	skeletonBook := bookOf(skeleton)
	if skeletonBook["files"] != nil {
		t.Fatal("default book response must be the skeleton (no files)")
	}
	for _, unit := range skeletonBook["units"].([]any) {
		u := unit.(map[string]any)
		if u["outlineRaw"] != nil || u["proseRaw"] != nil {
			t.Fatalf("skeleton kept unit raw text: %v", u["unitId"])
		}
	}

	// include=files keeps panel documents but no prose.
	filesBook := bookOf(files)
	filesMap, ok := filesBook["files"].(map[string]any)
	if !ok || len(filesMap) == 0 {
		t.Fatal("include=files lost panel documents")
	}
	for name := range filesMap {
		if strings.HasPrefix(name, "units/") || strings.HasPrefix(name, "backups/") {
			t.Fatalf("include=files kept non-panel file %q", name)
		}
	}
	if _, ok := filesMap["book-bible.md"]; !ok {
		t.Fatal("include=files lost book-bible.md")
	}
	for _, unit := range filesBook["units"].([]any) {
		u := unit.(map[string]any)
		if u["outlineRaw"] != nil || u["proseRaw"] != nil {
			t.Fatalf("include=files restored unit raws: %v", u["unitId"])
		}
	}
	// Panel documents add weight over the skeleton but stay far below a full
	// snapshot (the skeleton is a tiny fraction of the prose-heavy book).
	if len(filesRaw) <= len(skeletonRaw) {
		t.Fatalf("include=files did not add panel documents: skeleton=%d files=%d", len(skeletonRaw), len(filesRaw))
	}
	// Primary action and continuity survive both shapes.
	for name, body := range map[string]map[string]any{"skeleton": skeleton, "files": files} {
		if body["primary"] == nil {
			t.Fatalf("%s response lost primary action", name)
		}
	}
}

func TestRunListProjections(t *testing.T) {
	server, store, bookID := projectionFixture(t)

	// Slim-only: the run list always carries precomputed totals, never calls.
	res, err := http.Get(server.URL + "/api/books/" + bookID + "/runs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var summary []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if len(summary) != 1 {
		t.Fatalf("expected 1 run, got %d", len(summary))
	}
	run := summary[0]
	if run["calls"] != nil || run["contextSummary"] != nil {
		t.Fatal("run list kept per-call records")
	}
	// Precomputed totals: 1000+1100 in, 500+50 out, 800+1050 cache read, 40 creation, 1500ms.
	checks := map[string]float64{"inputTokens": 2100, "outputTokens": 550, "cacheRead": 1850, "cacheCreation": 40, "durationMs": 1500}
	for key, want := range checks {
		if got, _ := run[key].(float64); got != want {
			t.Fatalf("summary %s = %v, want %v", key, run[key], want)
		}
	}

	// The full per-call detail is only on the single-run endpoint.
	resDetail, err := http.Get(server.URL + "/api/runs/" + run["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer resDetail.Body.Close()
	var detail map[string]any
	if err := json.NewDecoder(resDetail.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail["calls"] == nil {
		t.Fatal("single-run endpoint lost calls")
	}

	// limit/offset page through the list.
	for i := 2; i <= 4; i++ {
		extra := domain.Run{
			ID: fmt.Sprintf("run-%d", i), BookID: bookID, Kind: domain.TaskReview, Status: domain.RunSucceeded,
			ModelID: "mock/model", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := store.SaveRun(context.Background(), extra); err != nil {
			t.Fatal(err)
		}
	}
	resPage, err := http.Get(server.URL + "/api/runs?bookId=" + bookID + "&limit=2&offset=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resPage.Body.Close()
	var page []map[string]any
	if err := json.NewDecoder(resPage.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("limit=2 returned %d runs", len(page))
	}
}

func TestCandidateListProjections(t *testing.T) {
	server, _, bookID := projectionFixture(t)

	// Slim-only: the candidate list always strips bulk payloads.
	res, err := http.Get(server.URL + "/api/books/" + bookID + "/candidates")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var summary []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if len(summary) != 2 {
		t.Fatalf("expected 2 open candidates, got %d", len(summary))
	}
	byID := map[string]map[string]any{}
	for _, c := range summary {
		byID[c["id"].(string)] = c
	}
	write := byID["cand-1"]
	if write["chapters"] != nil || write["baseChapters"] != nil {
		t.Fatal("candidate list kept prose payloads")
	}
	if write["hasChapters"] != true || write["hasBaseChapters"] != true {
		t.Fatal("candidate list lost Has* flags")
	}
	review := byID["cand-review"]
	findings, ok := review["findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatal("candidate list lost findings (review panel renders from the list)")
	}

	// Detail endpoint keeps everything.
	_, detail := getJSON(t, server, "/api/candidates/cand-1")
	if detail["chapters"] == nil || detail["baseChapters"] == nil {
		t.Fatal("candidate detail lost payloads")
	}

	// History: kind filter + stripped chapters.
	resHist, err := http.Get(server.URL + "/api/books/" + bookID + "/candidates/history?kind=review")
	if err != nil {
		t.Fatal(err)
	}
	defer resHist.Body.Close()
	var history []map[string]any
	if err := json.NewDecoder(resHist.Body).Decode(&history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0]["id"] != "cand-archived" {
		t.Fatalf("kind=review history filter failed: %v", history)
	}
	if history[0]["chapters"] != nil {
		t.Fatal("history kept reviewed prose")
	}
}
