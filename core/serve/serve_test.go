package serve

import (
	"context"
	"net/http"
	"testing"
)

func TestDynamicPortAndInProcessContract(t *testing.T) {
	t.Setenv("NOVEL_DB_PATH", "")
	t.Setenv("NOVEL_LIBRARY", "")
	core, err := New(Options{Home: t.TempDir(), Host: "127.0.0.1", DynamicPort: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Start(); err != nil {
		t.Fatal(err)
	}
	if core.BaseURL() == "" || core.BaseURL() == "http://127.0.0.1:7804" {
		t.Fatalf("expected a dynamically allocated port, got %s", core.BaseURL())
	}
	res, err := http.Get(core.BaseURL() + "/api/system/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("health status %d", res.StatusCode)
	}
	if err := core.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultDesktopPortCompatibility(t *testing.T) {
	// The default is still 7804; this test only constructs the core and closes
	// it without binding, so an active developer server does not interfere.
	core, err := New(Options{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer core.Stop(context.Background())
	if core.port != 7804 {
		t.Fatalf("desktop default port changed to %d", core.port)
	}
	if core.BaseURL() != "" {
		t.Fatal("in-process core must not expose a listener")
	}
}
