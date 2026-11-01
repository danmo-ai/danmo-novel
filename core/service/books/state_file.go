package books

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
)

// StatePath is the machine-readable continuity ledger.
//
// 它是"随剧情变化的事实"的唯一存放处：位置、伤势、资源、目标、知情范围、
// 伏笔状态。静态人设仍在 canon/cast/，连载位置仍在 novel-state.yaml 顶层，
// 三者不重复。
const StatePath = "continuity/state.yaml"

// LoadState reads continuity/state.yaml. A missing file is not an error: a new
// book simply has no finalized unit yet, and the caller should render
// "no continuity yet" rather than fail the run.
func LoadState(root string) ([]domain.UnitState, error) {
	raw, err := os.ReadFile(filepath.Join(root, StatePath))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	var f domain.StateFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", StatePath, err)
	}
	sortStates(f.Units)
	return f.Units, nil
}

// StateAt returns the snapshot in force when entering unitID: the last segment
// strictly before it. "Strictly before" is what makes 续写 and 重写 behave the
// same — rewriting v02-U1 must see v01-U8's state, never v02-U1's own output.
func StateAt(states []domain.UnitState, unitID string) (domain.UnitState, bool) {
	key := UnitOrderKey(unitID)
	if key == 0 {
		return domain.UnitState{}, false
	}
	var best domain.UnitState
	found := false
	for _, st := range states {
		if UnitOrderKey(st.Unit) >= key {
			continue
		}
		if !found || UnitOrderKey(st.Unit) > UnitOrderKey(best.Unit) {
			best, found = st, true
		}
	}
	return best, found
}

// AppendUnitState writes or replaces one unit's snapshot, keeping the file
// ordered by unit. Re-finalizing a unit overwrites its own segment instead of
// appending a duplicate — the ledger holds one row per unit.
func AppendUnitState(root string, st domain.UnitState) error {
	if strings.TrimSpace(st.Unit) == "" {
		return fmt.Errorf("unit state without unit id")
	}
	states, err := LoadState(root)
	if err != nil {
		return err
	}
	replaced := false
	for i := range states {
		if states[i].Unit == st.Unit {
			states[i] = st
			replaced = true
			break
		}
	}
	if !replaced {
		states = append(states, st)
	}
	sortStates(states)
	return writeState(root, states)
}

// ReplaceState rewrites the whole ledger. Used by the migration command.
func ReplaceState(root string, states []domain.UnitState) error {
	sortStates(states)
	return writeState(root, states)
}

func writeState(root string, states []domain.UnitState) error {
	path := filepath.Join(root, StatePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f := domain.StateFile{Version: 1, Units: states}
	var b strings.Builder
	b.WriteString("# 连续性状态账本（机器可读，按单元段）\n")
	b.WriteString("# 每个已定稿单元一段；读取时取目标单元之前最近的一段。\n")
	b.WriteString("# 只存随剧情变化的事实：位置、伤势、资源、目标、知情范围、伏笔。\n")
	b.WriteString("# 静态人设见 canon/cast/，连载位置见 novel-state.yaml 顶层。\n\n")
	enc, err := yaml.Marshal(&f)
	if err != nil {
		return err
	}
	b.Write(enc)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func sortStates(states []domain.UnitState) {
	sort.SliceStable(states, func(i, j int) bool {
		return UnitOrderKey(states[i].Unit) < UnitOrderKey(states[j].Unit)
	})
}

// StateFromOutline builds a snapshot from a unit's final outline.
//
// state_deltas 是细纲里声明的"本单元结束后哪些事实变了"，是唯一权威来源：
// 定稿时按它落段，而不是再写一套增量文本追加（旧 facts.md 就是这么腐化的）。
func StateFromOutline(unit domain.UnitInfo, chapters []domain.ChapterDraft, identities ...[]domain.IdentitySegment) domain.UnitState {
	st := domain.UnitState{Unit: unit.UnitID, Volume: unit.Volume}
	if len(chapters) > 0 {
		st.AfterCh = chapters[len(chapters)-1].Chapter
	}
	if day := yamlScalarValue(unit.OutlineRaw, "story_day"); day != "" {
		if n := atoiSafe(day); n > 0 {
			st.StoryDay = n
		}
	}
	cast := map[string]domain.CharacterSnapshot{}
	for _, d := range parseStateDeltas(unit.OutlineRaw) {
		if d.Stem == "" {
			continue
		}
		snap := cast[d.Stem]
		switch d.Field {
		case "location", "position":
			snap.Location = d.To
		case "injury":
			snap.Injury = d.To
		case "resources":
			snap.Resources = d.To
		case "goal":
			snap.Goal = d.To
		case "role_title", "identity", "title":
			// Public identity is owned by identity-timeline, not by a prose
			// delta. A next-unit promotion must not rewrite this unit's snapshot.
			if len(identities) > 0 {
				if identity := IdentityAt(identities[0], d.Stem, unit.UnitID); identity != nil {
					snap.RoleTitle = identity.RoleTitle
				}
			}
		case "knowledge", "knows":
			if strings.TrimSpace(d.To) != "" {
				snap.Knows = append(snap.Knows, d.To)
			}
		}
		cast[d.Stem] = snap
	}
	if len(cast) > 0 {
		// 只保留真正记到东西的人：空映射在提示词里是一条没有信息的噪音行。
		for stem, snap := range cast {
			if snap.RoleTitle == "" && snap.Location == "" && snap.Injury == "" &&
				snap.Resources == "" && snap.Goal == "" && len(snap.Knows) == 0 {
				delete(cast, stem)
			}
		}
	}
	if len(cast) > 0 {
		st.Cast = cast
	}
	return st
}

// stateDelta is one flat "field went from A to B" record.
type stateDelta struct {
	Stem  string
	Field string
	From  string
	To    string
}

// parseStateDeltas reads the unit's state_deltas block.
//
// 兼容两种形态：细纲正文里的 YAML 列表，以及旧版序列化写出的
// state_deltas_json 字符串。后者曾是生产者/消费者的字段名不一致点。
func parseStateDeltas(raw string) []stateDelta {
	// A structured volume seed may be finalized before a separate outline
	// rewrite. Its compact state_changes must still reach the same ledger.
	if value := yamlScalarValue(raw, "state_changes"); value != "" {
		if rows := stateDeltasFromMarkdown(value); len(rows) > 0 {
			return rowsToDeltas(markdownDeltaRows(rows))
		}
	}
	if block := mdYAMLSection(raw, "state_deltas"); block != "" {
		var rows []map[string]any
		if err := yaml.Unmarshal([]byte(block), &rows); err == nil && len(rows) > 0 {
			return rowsToDeltas(rows)
		}
	}
	if s := yamlScalarValue(raw, "state_deltas_json"); s != "" {
		var rows []map[string]any
		if err := yaml.Unmarshal([]byte(s), &rows); err == nil && len(rows) > 0 {
			return rowsToDeltas(rows)
		}
	}
	// Do not silently lose a planned state change just because the current
	// unit writer used a different YAML spelling. The finalizer must see it.
	if value := yamlScalarValue(raw, "state_changes"); value != "" {
		if rows := stateDeltasFromMarkdown(value); len(rows) > 0 {
			return rowsToDeltas(markdownDeltaRows(rows))
		}
	}
	return nil
}

func markdownDeltaRows(rows []map[string]string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		converted := map[string]any{}
		for key, value := range row {
			converted[key] = value
		}
		out = append(out, converted)
	}
	return out
}

func rowsToDeltas(rows []map[string]any) []stateDelta {
	out := make([]stateDelta, 0, len(rows))
	for _, row := range rows {
		str := func(k string) string {
			if v, ok := row[k]; ok && v != nil {
				return strings.TrimSpace(fmt.Sprintf("%v", v))
			}
			return ""
		}
		d := stateDelta{Stem: str("stem"), Field: str("field"), From: str("from"), To: str("to")}
		if d.Stem != "" && d.Field != "" {
			out = append(out, d)
		}
	}
	return out
}

func yamlScalarValue(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, key+":") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))
		v = strings.Trim(v, `"'`)
		return v
	}
	return ""
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
