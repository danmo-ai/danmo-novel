package books

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPanelDocumentWhitelist(t *testing.T) {
	keep := []string{
		"book-bible.md", "novel-state.yaml",
		"outline/book_outline.md", "outline/volumes/v01.md", "outline/units/v01-U1.yaml",
		"canon/world.md", "canon/cast/lin.md", "canon/identity-timeline.yaml",
		"continuity/state.yaml", "continuity/commits/v01-U1.md",
	}
	drop := []string{
		"units/v01-U1.md", "backups/pre-migrate/x.md", "README.md",
		"../outside.md", "canon/../secret.md", "outline/./../units/v01-U1.md",
	}
	for _, name := range keep {
		if !isPanelDocument(name) {
			t.Fatalf("whitelist dropped panel document %q", name)
		}
	}
	for _, name := range drop {
		if isPanelDocument(name) {
			t.Fatalf("whitelist kept non-panel file %q", name)
		}
	}
}

// include=files is the panel-document tier: navigation skeleton plus exactly
// the documents the outline/bible panels render. It must stay smaller than the
// full snapshot while restoring what the skeleton stripped for the panels.
func TestPanelDocumentsRestoreOntoSkeleton(t *testing.T) {
	root, _, err := CreateDemoBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	skeleton := WithListingView(snap)
	combined := WithPanelDocuments(skeleton, snap)

	if combined.Files == nil {
		t.Fatal("combined view has no files")
	}
	for name := range combined.Files {
		if !isPanelDocument(name) {
			t.Fatalf("combined view kept non-panel file %q", name)
		}
	}
	if _, ok := combined.Files["book-bible.md"]; !ok {
		t.Fatal("combined view lost book-bible.md")
	}
	if combined.StateRaw == "" && snap.StateRaw != "" {
		t.Fatal("combined view lost state raw")
	}
	for _, unit := range combined.Units {
		if unit.OutlineRaw != "" || unit.ProseRaw != "" {
			t.Fatalf("combined view restored unit raw text for %q", unit.UnitID)
		}
	}
	combinedJSON, _ := json.Marshal(combined)
	fullJSON, _ := json.Marshal(snap)
	if len(combinedJSON) >= len(fullJSON) {
		t.Fatalf("combined view did not shrink payload: full=%d combined=%d", len(fullJSON), len(combinedJSON))
	}
	// Prose paths are what the demo book's bulk is made of; none may leak in.
	for name := range combined.Files {
		if strings.HasPrefix(name, "units/") {
			t.Fatalf("combined view leaked prose file %q", name)
		}
	}
}
