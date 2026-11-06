package books

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
)

type actualFile struct {
	Version  int                    `yaml:"version"`
	Unit     string                 `yaml:"unit"`
	Chapters []domain.ChapterActual `yaml:"chapters"`
}

// ActualPath stores author-accepted facts derived from the finalized prose.
// It is an index of that prose, not a planning document or a second canon.
func ActualPath(unitID string) string { return "continuity/actual/" + unitID + ".yaml" }

func MarshalActual(unitID string, chapters []domain.ChapterActual) ([]byte, error) {
	return yaml.Marshal(actualFile{Version: 1, Unit: unitID, Chapters: chapters})
}

func LoadActual(root, unitID string) ([]domain.ChapterActual, error) {
	raw, err := os.ReadFile(filepath.Join(root, ActualPath(unitID)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file actualFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", ActualPath(unitID), err)
	}
	return file.Chapters, nil
}

// StateFromActual merges only author-confirmed, prose-backed transitions.
func StateFromActual(base domain.UnitState, unit domain.UnitInfo, chapters []domain.ChapterActual) (domain.UnitState, error) {
	st := StateClockFromUnit(base, unit, nil)
	if len(chapters) > 0 {
		st.AfterCh = chapters[len(chapters)-1].Chapter
		for _, ch := range chapters {
			if ch.StoryDay > 0 {
				st.StoryDay = ch.StoryDay
			}
			if ch.EndDay >= st.StoryDay {
				st.EndDay = ch.EndDay
			}
		}
	}
	for _, ch := range chapters {
		for _, id := range ch.Plant {
			if !containsString(ch.PlannedForeshadowHandled, id) {
				return domain.UnitState{}, fmt.Errorf("第 %d 章的伏笔 %s 未明确核对", ch.Chapter, id)
			}
		}
		for _, id := range ch.Payoff {
			if !containsString(ch.PlannedForeshadowHandled, id) {
				return domain.UnitState{}, fmt.Errorf("第 %d 章的伏笔 %s 未明确核对", ch.Chapter, id)
			}
		}
		for _, d := range ch.StateDeltas {
			if !d.Confirmed || strings.TrimSpace(d.Evidence) == "" {
				continue
			}
			row := domain.CanonicalStateField(d.Field)
			if row == "" || domain.OwnedByIdentityTimeline(row) {
				return domain.UnitState{}, fmt.Errorf("实际账本 %s 不支持字段 %s", unit.UnitID, d.Field)
			}
			entry := st.Cast[d.Stem]
			switch row {
			case "location":
				entry.Location = d.To
			case "health":
				entry.Health = d.To
			case "resources":
				entry.Resources = d.To
			case "goal":
				entry.Goal = d.To
			case "relation":
				entry.Relation = d.To
			case "life":
				entry.Life = domain.CanonicalLife(d.To)
			case "knows":
				if !containsString(entry.Knows, d.To) {
					entry.Knows = append(entry.Knows, d.To)
				}
			}
			if st.Cast == nil {
				st.Cast = map[string]domain.CharacterSnapshot{}
			}
			st.Cast[d.Stem] = entry
		}
	}
	// Replay the actual ledger's IDs and chapter numbers, not chapter plans.
	loops := append([]domain.LoopEntry(nil), base.Loops...)
	index := map[string]int{}
	upsert := func(id string) *domain.LoopEntry {
		if i, ok := index[id]; ok {
			return &loops[i]
		}
		loops = append(loops, domain.LoopEntry{ID: id, Status: "planted"})
		index[id] = len(loops) - 1
		return &loops[len(loops)-1]
	}
	for _, ch := range chapters {
		for _, id := range ch.Plant {
			e := upsert(id)
			if e.Planted == 0 {
				e.Planted = ch.Chapter
			}
			if e.Status != "paid" {
				e.Status = "planted"
			}
		}
		for _, id := range ch.Payoff {
			e := upsert(id)
			e.Status, e.Paid = "paid", ch.Chapter
		}
	}
	sort.Slice(loops, func(i, j int) bool { return loops[i].ID < loops[j].ID })
	st.Loops = loops
	return st, nil
}

// ProjectVolumeSummary builds the model-facing chapter index from actual facts.
// No summaries file is written at finalize time. Legacy rows fill only chapters
// that predate actual-ledger tracking; actual chapters always win by chapter ID.
func ProjectVolumeSummary(root string, volume int, beforeUnitID string) (string, error) {
	snap, err := LoadBook(root, false)
	if err != nil {
		return "", err
	}
	type summaryRow struct {
		chapter int
		event   string
		loops   string
	}
	rows := map[int]summaryRow{}
	for _, unit := range snap.Units {
		if unit.Volume != volume || (beforeUnitID != "" && UnitOrderKey(unit.UnitID) >= UnitOrderKey(beforeUnitID)) {
			continue
		}
		actual, err := LoadActual(root, unit.UnitID)
		if err != nil {
			return "", err
		}
		for _, ch := range actual {
			loops := append([]string{}, ch.Plant...)
			for _, id := range ch.Payoff {
				loops = append(loops, id+"→已收")
			}
			if len(loops) == 0 {
				loops = []string{"—"}
			}
			rows[ch.Chapter] = summaryRow{chapter: ch.Chapter, event: ch.Summary, loops: strings.Join(loops, "；")}
		}
	}
	// Legacy summaries retain only their event column; dynamic character state
	// comes from state.yaml and is never copied into this projection.
	legacyPath := filepath.Join(root, "continuity", "summaries", fmt.Sprintf("v%02d.md", volume))
	if raw, readErr := os.ReadFile(legacyPath); readErr == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			match := chapterRowRe.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			chapter, parseErr := strconv.Atoi(match[1])
			if parseErr != nil {
				continue
			}
			if _, exists := rows[chapter]; exists {
				continue
			}
			cells := strings.Split(strings.Trim(line, "|"), "|")
			if len(cells) < 2 {
				continue
			}
			rows[chapter] = summaryRow{chapter: chapter, event: strings.TrimSpace(strings.ReplaceAll(cells[1], "\\|", "|")), loops: "—"}
		}
	} else if !os.IsNotExist(readErr) {
		return "", readErr
	}
	if len(rows) == 0 {
		return "", nil
	}
	ordered := make([]summaryRow, 0, len(rows))
	for _, row := range rows {
		ordered = append(ordered, row)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].chapter < ordered[j].chapter })
	var out strings.Builder
	fmt.Fprintf(&out, "# 第 %d 卷实际章摘要（由 actual 派生）\n\n| 章 | 实际事件 | 实际伏笔 |\n|----|----------|----------|\n", volume)
	for _, row := range ordered {
		fmt.Fprintf(&out, "| %d | %s | %s |\n", row.chapter, markdownCell(row.event), markdownCell(row.loops))
	}
	return out.String(), nil
}

// RebuildStateFromActual deterministically replays all contiguous accepted ledgers.
// A gap is an error, never permission to delete the target or later state.
func RebuildStateFromActual(root, unitID string, current []domain.ChapterActual) ([]domain.UnitState, error) {
	snap, err := LoadBook(root, false)
	if err != nil {
		return nil, err
	}
	states, err := LoadState(root)
	if err != nil {
		return nil, err
	}
	cur, _ := StateAt(states, unitID)
	key := UnitOrderKey(unitID)
	kept := []domain.UnitState{}
	for _, st := range states {
		if UnitOrderKey(st.Unit) < key {
			kept = append(kept, st)
		}
	}
	byID := map[string]domain.UnitState{}
	for _, st := range states {
		byID[st.Unit] = st
	}
	units := append([]domain.UnitInfo(nil), snap.Units...)
	sort.Slice(units, func(i, j int) bool { return UnitOrderKey(units[i].UnitID) < UnitOrderKey(units[j].UnitID) })
	started := false
	for _, u := range units {
		if UnitOrderKey(u.UnitID) < key {
			continue
		}
		actual := current
		if u.UnitID != unitID {
			if _, ok := byID[u.UnitID]; !ok {
				break
			}
			actual, err = LoadActual(root, u.UnitID)
			if err != nil {
				return nil, err
			}
			if len(actual) == 0 {
				// Historical ledgers created before actual-ledger tracking retain their
				// existing state; their derived facts are not re-inferred here.
				kept = append(kept, byID[u.UnitID])
				cur = byID[u.UnitID]
				continue
			}
		}
		started = true
		cur, err = StateFromActual(cur, u, actual)
		if err != nil {
			return nil, err
		}
		kept = append(kept, cur)
	}
	if !started {
		return nil, fmt.Errorf("没有可重建的定稿账本 %s", unitID)
	}
	return kept, nil
}
