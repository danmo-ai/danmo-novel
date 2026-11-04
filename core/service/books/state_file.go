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

// RebuildStateFrom rewrites the segment for unitID and every later unit that// already has one, back-to-back from the snapshot in force when entering unitID.
//
// 为什么必须级联重算：每段都是全量快照，下游段里已经冻结了「由旧的上游状态
// 推导出来」的值。重写 U5 只替换 U5 自己的段，U6/U7 仍带着旧的 U5 结果，
// 而 U7 写作时读到的正是 U6——它看起来完好，实际已经过期，且没有任何标记能
// 提示这一点。重算让下游段始终等于「按当前上游状态 + 各自 deltas」推导的结果。
//
// 只重算**已有段**的单元：从未定稿的单元不该凭空产生状态（那会假装它写完了）。
// 遇到第一个没有段的单元就停止传播，因为它之后的单元理论上也还没定稿。
func RebuildStateFrom(root, unitID string, identities ...[]domain.IdentitySegment) error {
	snap, err := LoadBook(root, false)
	if err != nil {
		return err
	}
	states, err := LoadState(root)
	if err != nil {
		return err
	}
	key := UnitOrderKey(unitID)
	if key == 0 {
		return fmt.Errorf("invalid unit id: %s", unitID)
	}
	had := map[string]bool{}
	prevAfterCh := map[string]int{}
	kept := make([]domain.UnitState, 0, len(states))
	for _, st := range states {
		had[st.Unit] = true
		prevAfterCh[st.Unit] = st.AfterCh
		if UnitOrderKey(st.Unit) < key {
			kept = append(kept, st)
		}
	}
	cur, _ := StateAt(states, unitID)

	units := append([]domain.UnitInfo(nil), snap.Units...)
	sort.SliceStable(units, func(i, j int) bool {
		return UnitOrderKey(units[i].UnitID) < UnitOrderKey(units[j].UnitID)
	})
	for _, u := range units {
		if UnitOrderKey(u.UnitID) < key {
			continue
		}
		// 本次定稿的单元无条件重建；下游只重建已经定稿过的。
		// 遇到第一个没有段的单元就停止：它之后的单元也还没定稿，不能凭空造状态。
		if u.UnitID != unitID && !had[u.UnitID] {
			break
		}
		chapters := []domain.ChapterDraft{{Chapter: u.ChapterTo}}
		if u.ChapterTo <= 0 {
			chapters = nil
		}
		st := StateFromOutlineWithBase(cur, u, chapters, identities...)
		// after_ch 是「实际定稿到第几章」的执行事实，不该因为重建被改掉：
		// 一个单元可能只写完了前半部分就定稿过。已有值优先。
		if prior := prevAfterCh[u.UnitID]; prior > 0 {
			st.AfterCh = prior
		}
		kept = append(kept, st)
		cur = st
	}
	sortStates(kept)
	return writeState(root, kept)
}

func marshalState(states []domain.UnitState) ([]byte, error) {
	f := domain.StateFile{Version: 1, Units: states}
	var b strings.Builder
	b.WriteString("# 连续性状态账本（机器可读，按单元段）\n")
	b.WriteString("# 每个已定稿单元一段，每段都是全量快照：读取时取目标单元之前最近的一段即可。\n")
	b.WriteString("# 只存随剧情变化的事实：位置、身体状态、资源、目标、关系、生命状态、知情范围、伏笔。\n")
	b.WriteString("# 静态人设见 canon/cast/，连载位置见 novel-state.yaml 顶层。\n\n")
	enc, err := yaml.Marshal(&f)
	if err != nil {
		return nil, err
	}
	b.Write(enc)
	return []byte(b.String()), nil
}

func writeState(root string, states []domain.UnitState) error {
	body, err := marshalState(states)
	if err != nil {
		return err
	}
	path := filepath.Join(root, StatePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func sortStates(states []domain.UnitState) {
	sort.SliceStable(states, func(i, j int) bool {
		return UnitOrderKey(states[i].Unit) < UnitOrderKey(states[j].Unit)
	})
}

// StateClockFromUnit carries entry state and planned clock only; no state deltas.
func StateClockFromUnit(base domain.UnitState, unit domain.UnitInfo, chapters []domain.ChapterDraft) domain.UnitState {
	st := base
	st.Unit, st.Volume = unit.UnitID, unit.Volume
	st.AfterCh = 0
	if len(chapters) > 0 {
		st.AfterCh = chapters[len(chapters)-1].Chapter
	}
	if day := yamlScalarValue(unit.OutlineRaw, "story_day"); day != "" {
		if n := atoiSafe(day); n > 0 {
			st.StoryDay = n
		}
	}
	st.EndDay = st.StoryDay
	if day := yamlScalarValue(unit.OutlineRaw, "end_day"); day != "" {
		if n := atoiSafe(day); n >= st.StoryDay {
			st.EndDay = n
		}
	}
	return st
}

// StateFromOutline builds a snapshot from a unit's final outline.
//
// state_deltas 是细纲里声明的"本单元结束后哪些事实变了"，是唯一权威来源：
// 定稿时按它落段，而不是再写一套增量文本追加（旧 facts.md 就是这么腐化的）。
//
// 它只返回本单元的变化项，供不关心历史的调用方使用。定稿必须走
// StateFromOutlineWithBase：账本的每一段都是全量快照，读取才能只看最近一段。
func StateFromOutline(unit domain.UnitInfo, chapters []domain.ChapterDraft, identities ...[]domain.IdentitySegment) domain.UnitState {
	return StateFromOutlineWithBase(domain.UnitState{}, unit, chapters, identities...)
}

// StateFromOutlineWithBase merges this unit's deltas onto the previous snapshot
// and returns the **full** state.
//
// 每一段都写全量，是因为读取侧只取"进入目标单元前最近的一段"。若每段只写变化
// 项，U3 就看不到 U1 记下的伤势——不是因为它过期，而是结构上不携带。以前靠
// 读时合并补救，代价是把过期值也一起捡回来；改为落盘时合并，读取侧保持简单。
func StateFromOutlineWithBase(base domain.UnitState, unit domain.UnitInfo, chapters []domain.ChapterDraft, identities ...[]domain.IdentitySegment) domain.UnitState {
	st := StateClockFromUnit(base, unit, chapters)
	cast := map[string]domain.CharacterSnapshot{}
	for stem, snap := range st.Cast {
		cast[stem] = snap
	}
	for _, d := range parseStateDeltas(unit.OutlineRaw) {
		if d.Stem == "" {
			continue
		}
		snap := cast[d.Stem]
		switch domain.CanonicalStateField(d.Field) {
		case "location":
			snap.Location = d.To
		case "health":
			snap.Health = d.To
		case "resources":
			snap.Resources = d.To
		case "goal":
			snap.Goal = d.To
		case "relation":
			snap.Relation = d.To
		case "life":
			snap.Life = domain.CanonicalLife(d.To)
		case "role_title":
			// 身份由 canon/identity-timeline.yaml 独占，账本不再存副本。
			//
			// 这条 delta 仍然有意义：它是「本单元在边界上切了身份」的声明，
			// stateDeltaIdentityConflict 会拿它跟时间线比对（to 必须等于本单元
			// 生效身份）。但落盘时不抄进快照——曾经抄过一份，实测 7 段里只有
			// 1 段有值，而且人工修正时间线后副本不会跟着变，同一份提示词里
			// 同一个人出现两个身份。现在身份只在时间线里存一处。
			continue
		case "knows":
			// 知情范围是累积集合，不是覆盖值：补一条不抹掉之前知道的。
			if to := strings.TrimSpace(d.To); to != "" && !containsString(snap.Knows, to) {
				snap.Knows = append(snap.Knows, to)
			}
		default:
			// 未识别的 field 由 ValidateStateDeltas 在细纲接受时拦下，
			// 这里不静默吞掉也不再猜语义——猜错比丢弃更难查。
			continue
		}
		cast[d.Stem] = snap
	}
	if len(cast) > 0 {
		// 只保留真正记到东西的人：空映射在提示词里是一条没有信息的噪音行。
		for stem, snap := range cast {
			if snap.Location == "" && snap.Health == "" &&
				snap.Resources == "" && snap.Goal == "" && snap.Relation == "" &&
				snap.Life == "" && len(snap.Knows) == 0 {
				delete(cast, stem)
			}
		}
	}
	st.Cast = cast
	if len(cast) == 0 {
		st.Cast = nil
	}
	st.Loops = mergeUnitLoops(st.Loops, unit)
	return st
}

// mergeUnitLoops 把本单元章合同的 plant/payoff 折算成伏笔状态。
//
// 账本此前有 Loops 字段、有渲染器（continuityLoops），却从来没人写入——
// 于是「未回收伏笔」这一块在写作包里从未真正出现过。定稿是唯一知道
// 「实际在第几章埋、第几章收」的时刻，就在这里落段。
//
// 状态只升不降：已经 paid 的不会因为重写单元而退回 planted。
func mergeUnitLoops(base []domain.LoopEntry, unit domain.UnitInfo) []domain.LoopEntry {
	planted, paid := ChapterLoops(unit)
	if len(planted) == 0 && len(paid) == 0 {
		return base
	}
	index := map[string]int{}
	for i, loop := range base {
		index[loop.ID] = i
	}
	upsert := func(id string, apply func(*domain.LoopEntry)) {
		if i, ok := index[id]; ok {
			apply(&base[i])
			return
		}
		entry := domain.LoopEntry{ID: id, Status: "planted"}
		apply(&entry)
		base = append(base, entry)
		index[id] = len(base) - 1
	}
	for ch := unit.ChapterFrom; ch <= unit.ChapterTo; ch++ {
		for _, id := range planted[ch] {
			upsert(id, func(e *domain.LoopEntry) {
				if e.Planted == 0 {
					e.Planted = ch
				}
				if !strings.EqualFold(e.Status, "paid") {
					e.Status = "planted"
				}
			})
		}
		for _, id := range paid[ch] {
			upsert(id, func(e *domain.LoopEntry) {
				e.Status = "paid"
				e.Paid = ch
			})
		}
	}
	sort.SliceStable(base, func(i, j int) bool { return base[i].ID < base[j].ID })
	return base
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// UnknownStateDeltaFields lists the field names used by a unit's state_deltas
// that are not in domain.StateDeltaFields.
//
// 这是过去 70% 写入被丢弃的根因：提示词与识别分支各写一份词表，模型写了
// status/method/evidence 也照样"成功"落盘，然后被无 default 的 switch 扔掉。
// 现在把它变成可校验的事实，在细纲接受时进修复轮。
func UnknownStateDeltaFields(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range parseStateDeltas(raw) {
		if strings.TrimSpace(d.Field) == "" {
			continue
		}
		if domain.CanonicalStateField(d.Field) != "" {
			continue
		}
		name := strings.TrimSpace(d.Field)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// stateDelta is one flat "field went from A to B" record.
type stateDelta struct {
	Stem  string
	Field string
	From  string
	To    string
}

// parseStateDeltas reads the unit's state_deltas block only.
//
// 单元的状态事实只认结构化 state_deltas。以前还会回退读卷纲种子的
// `state_changes: stem.field=前->后`：那条路径会把「计划」当成「实际」，
// 尤其当单元明确写了 `state_deltas: []` 时仍被计划覆盖。现在计划只用于
// 父子合同校验（ContractStateDeltaKeys），不进入状态重建。
func parseStateDeltas(raw string) []stateDelta {
	block := mdYAMLSection(raw, "state_deltas")
	if block == "" {
		return nil
	}
	var rows []map[string]any
	if err := yaml.Unmarshal([]byte(block), &rows); err != nil {
		return nil
	}
	return rowsToDeltas(rows)
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

// ReplaceState rewrites the whole ledger (migration and repair paths).
func ReplaceState(root string, states []domain.UnitState) error {
	sortStates(states)
	return writeState(root, states)
}
