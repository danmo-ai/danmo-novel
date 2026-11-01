package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreResolvesImmutableProtocolAndOverlay(t *testing.T) {
	contract := NewContract("book-outline/v1", "markdown", "协议说明", "# 总纲\n- schema_version: 1\n")
	store := NewStore("")
	first, err := store.Resolve("plan_book", contract.ID, 1, contract)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Resolve("plan_book", contract.ID, 1, contract)
	if err != nil || first.Text != second.Text || first.Hash != second.Hash {
		t.Fatal("static prompt must be byte-identical across calls")
	}
	if !strings.Contains(first.Text, contract.Hash) || !strings.Contains(first.Text, contract.Template) {
		t.Fatal("output protocol was not injected into system prompt")
	}
	if strings.Contains(first.Text, "{{OUTPUT_PROTOCOL}}") {
		t.Fatal("unresolved protocol placeholder")
	}
}

func TestStoreRejectsMismatchedProtocol(t *testing.T) {
	contract := NewContract("wrong/v1", "markdown", "x", "y")
	if _, err := NewStore("").Resolve("plan_book", contract.ID, 1, contract); err == nil {
		t.Fatal("protocol mismatch must fail fast")
	}
}

func TestStoreRejectsOverlayChangingProtocol(t *testing.T) {
	dir := t.TempDir()
	body := "---\ntask: plan_book\nversion: 1\noutput_protocol: book-outline/v1\n---\n作者角色配置\n"
	if err := os.WriteFile(filepath.Join(dir, "plan_book.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	contract := NewContract("book-outline/v1", "markdown", "协议说明", "# 总纲\n")
	if _, err := NewStore(dir).Resolve("plan_book", contract.ID, 1, contract); err == nil || !strings.Contains(err.Error(), "OUTPUT_PROTOCOL") {
		t.Fatalf("overlay must not remove output protocol: %v", err)
	}
}
