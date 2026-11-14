package books

import "github.com/danmo-ai/danmo-novel/core/domain"

// WithListingView returns the navigation projection of a book: everything a
// unit list needs (summary, volume grouping, unit status/paths) and nothing
// else. It exists because the complete snapshot embeds every raw outline,
// prose body and structural projection — 3–4 MB for a 100+ unit book — and
// decoding that on a phone blocks the screen transition. Readers still fetch
// the one prose file they open through GET /books/:id/file.
//
// Fields that SelectPrimaryAction reads (Files, StateRaw) are dropped here, so
// callers must compute the primary action on the full snapshot first.
func WithListingView(snap domain.BookSnapshot) domain.BookSnapshot {
	snap.StateRaw = ""
	snap.Files = nil
	snap.StructuredOutlines = nil
	for i := range snap.Volumes {
		snap.Volumes[i].Raw = ""
		snap.Volumes[i].Structured = nil
	}
	for i := range snap.Units {
		snap.Units[i].OutlineRaw = ""
		snap.Units[i].ProseRaw = ""
	}
	for i := range snap.Cast {
		snap.Cast[i].Raw = ""
	}
	return snap
}
