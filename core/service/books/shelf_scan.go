package books

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// LoadBookSummary builds the shelf entry for one book without reading prose.
//
// The shelf only needs the state header, the unit outline files (which carry
// status and title) and whether a prose file exists. It does not need a single
// chapter body. Reading bodies here is what used to make GET /books slow and,
// when the library lives in iCloud, able to hang: a file that is not yet
// materialized on this device blocks os.ReadFile until iCloud downloads it,
// and one cold chapter body stalls the whole shelf behind it.
//
// Prose is therefore stat'ed, never read. Counts still match LoadBook because
// unit status comes from the outline and only "prose exists" is taken from
// the body's presence.
func LoadBookSummary(root string, readOnly bool) (domain.BookSummary, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return domain.BookSummary{}, err
	}
	if !info.IsDir() {
		return domain.BookSummary{}, fmt.Errorf("book root is not a directory: %s", root)
	}
	bookID := filepath.Base(root)
	stateRaw, _ := readFile(filepath.Join(root, "novel-state.yaml"))
	state := parseState(stateRaw)

	// Only outline/units/*.yaml is read: it is the metadata that decides unit
	// status, title and chapter range.
	files := map[string]string{}
	outlineDir := filepath.Join(root, "outline", "units")
	if entries, err := os.ReadDir(outlineDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !unitOutlineRe.MatchString(entry.Name()) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(outlineDir, entry.Name()))
			if err != nil {
				continue
			}
			files[filepath.ToSlash(filepath.Join("outline", "units", entry.Name()))] = string(raw)
		}
	}

	units := loadUnits(root, files)
	drafted, reviewed := 0, 0
	for i := range units {
		// Presence, not content: one stat cannot block on a download.
		if _, err := os.Stat(filepath.Join(root, units[i].ProsePath)); err == nil {
			units[i].HasProse = true
			// A unit whose outline still says "accepted" but whose prose has
			// landed is drafted; reviewed/finalized keep the outline's word.
			if units[i].Status == domain.UnitAccepted {
				units[i].Status = domain.UnitDrafted
			}
		}
		switch units[i].Status {
		case domain.UnitDrafted:
			drafted++
		case domain.UnitReviewed, domain.UnitFinalized:
			reviewed++
		}
	}

	title := state.Title
	if title == "" {
		// book-bible.md is small and the only fallback title source.
		bible, _ := readFile(filepath.Join(root, "book-bible.md"))
		title = firstMarkdownHeading(bible)
	}
	if title == "" {
		title = bookID
	}

	return domain.BookSummary{
		ID:              bookID,
		Title:           title,
		Genre:           state.Genre,
		Subgenre:        state.Subgenre,
		Stage:           state.Stage,
		LastCommittedCh: state.LastCommittedCh,
		NextAction:      state.NextAction,
		TimeSystem:      state.TimeSystem,
		UnitCount:       len(units),
		DraftedCount:    drafted,
		ReviewedCount:   reviewed,
		RootPath:        root,
		ReadOnly:        readOnly,
	}, nil
}