package books

import (
	"path"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// isPanelDocument is the include=files whitelist. Locked by tests: a missing
// entry shows up as an empty document in the outline/bible panel.
func isPanelDocument(name string) bool {
	clean := path.Clean(name)
	if clean != name || strings.HasPrefix(clean, "../") || strings.Contains(clean, "..") {
		return false
	}
	switch clean {
	case "book-bible.md", "novel-state.yaml":
		return true
	}
	for _, prefix := range []string{"outline/", "canon/", "continuity/"} {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	return false
}

// WithPanelDocuments restores the panel documents onto a listing skeleton.
// The default book response is the skeleton (Files/StateRaw/structured
// projections stripped); include=files adds back exactly what the
// outline/bible panels read, taken from the full snapshot. Unit raws stay
// stripped — the unit work surface fetches its own files — so the result is
// "navigation skeleton + panel documents", still far smaller than the full
// snapshot on a prose-heavy book.
func WithPanelDocuments(skeleton, full domain.BookSnapshot) domain.BookSnapshot {
	skeleton.StateRaw = full.StateRaw
	skeleton.StructuredOutlines = full.StructuredOutlines
	skeleton.IdentitySegments = full.IdentitySegments
	if full.Files != nil {
		kept := make(map[string]string, len(full.Files))
		for name, content := range full.Files {
			if isPanelDocument(name) {
				kept[name] = content
			}
		}
		skeleton.Files = kept
	}
	for i := range skeleton.Volumes {
		if i < len(full.Volumes) {
			skeleton.Volumes[i].Raw = full.Volumes[i].Raw
			skeleton.Volumes[i].Structured = full.Volumes[i].Structured
		}
	}
	for i := range skeleton.Cast {
		if i < len(full.Cast) {
			skeleton.Cast[i].Raw = full.Cast[i].Raw
		}
	}
	return skeleton
}
