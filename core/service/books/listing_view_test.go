package books

import (
	"encoding/json"
	"testing"
)

// The unit list on a phone only needs navigation metadata. The listing view is
// the difference between a 3–4 MB decode on every book tap and a small one, so
// assert both sides: payload shrinks, and what the list renders survives.
func TestListingViewKeepsNavigationAndDropsFullText(t *testing.T) {
	root, _, err := CreateDemoBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}

	full, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	slim := WithListingView(snap)
	slimJSON, err := json.Marshal(slim)
	if err != nil {
		t.Fatal(err)
	}

	if len(slimJSON) >= len(full) {
		t.Fatalf("listing view did not shrink payload: full=%d slim=%d", len(full), len(slimJSON))
	}
	if len(slim.Units) != len(snap.Units) || len(slim.Units) == 0 {
		t.Fatalf("listing view lost units: %d vs %d", len(slim.Units), len(snap.Units))
	}
	for i, unit := range slim.Units {
		original := snap.Units[i]
		if unit.UnitID == "" || unit.ProsePath == "" || unit.Volume == 0 {
			t.Fatalf("listing view dropped navigation fields for %q: %+v", unit.UnitID, unit)
		}
		if unit.OutlineRaw != "" || unit.ProseRaw != "" {
			t.Fatalf("listing view kept full text for %q", unit.UnitID)
		}
		if original.ProsePath != unit.ProsePath || original.HasProse != unit.HasProse {
			t.Fatalf("listing view changed navigation for %q", unit.UnitID)
		}
	}
	if slim.Files != nil || slim.StateRaw != "" || slim.StructuredOutlines != nil {
		t.Fatal("listing view kept bulk fields")
	}
}
