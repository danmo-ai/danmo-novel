package books

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// MigrationReport summarizes a one-off continuity migration.
type MigrationReport struct {
	UnitsRebuilt  []string
	UnitsSkipped  []string // no state_deltas to rebuild from
	FactsRemoved  bool
	NotesMoved    int // bytes of novel-state notes relocated
	StateSegments int
}

// MigrateContinuity rebuilds continuity/state.yaml from the units' state_deltas,
// retires the legacy facts.md, and moves the free-form novel-state notes into
// continuity/commits/book-notes.md.
//
// 幂等：已有同 unit 段则跳过；facts.md 不存在也算成功；notes 已迁则不再搬。
// 这是一次性迁移——旧账本的游标自相矛盾、人物快照三十多段从不合并，
// 与其写一堆兼容解析，不如按细纲重建一次。
func MigrateContinuity(root string) (MigrationReport, error) {
	var rep MigrationReport

	snap, err := LoadBook(root, false)
	if err != nil {
		return rep, err
	}
	existing, err := LoadState(root)
	if err != nil {
		return rep, err
	}
	have := map[string]bool{}
	for _, st := range existing {
		have[st.Unit] = true
	}

	for _, u := range snap.Units {
		if u.Status != domain.UnitReviewed && u.Status != domain.UnitFinalized {
			continue
		}
		if have[u.UnitID] {
			rep.UnitsSkipped = append(rep.UnitsSkipped, u.UnitID)
			continue
		}
		// 没有 state_deltas 就重建不出可信状态：不猜，交给作者重写细纲。
		if mdYAMLSection(u.OutlineRaw, "state_deltas") == "" && yamlScalarValue(u.OutlineRaw, "state_changes") == "" {
			rep.UnitsSkipped = append(rep.UnitsSkipped, u.UnitID)
			continue
		}
		chapters := []domain.ChapterDraft{{Chapter: u.ChapterTo}}
		// 必须带上前一段做基底：每段都应是全量快照。早期版本这里用
		// StateFromOutline（空基底），于是重建出来的账本仍是增量——
		// 实测 7 段里下载 5 段都丢了上游字段。
		base, _ := StateAt(existing, u.UnitID)
		st := StateFromOutlineWithBase(base, u, chapters)
		if len(st.Cast) == 0 {
			rep.UnitsSkipped = append(rep.UnitsSkipped, u.UnitID)
			continue
		}
		if err := AppendUnitState(root, st); err != nil {
			return rep, err
		}
		// 本段进入 existing，后续单元才能把它当基底。
		existing = append(existing, st)
		sortStates(existing)
		have[u.UnitID] = true
		rep.UnitsRebuilt = append(rep.UnitsRebuilt, u.UnitID)
	}

	if states, err := LoadState(root); err == nil {
		rep.StateSegments = len(states)
	}

	// facts.md 退出读写路径。删掉它：留着只会让人以为它仍是权威。
	factsPath := filepath.Join(root, "continuity", "facts.md")
	if err := os.Remove(factsPath); err == nil {
		rep.FactsRemoved = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return rep, err
	}

	n, err := moveStateNotes(root)
	if err != nil {
		return rep, err
	}
	rep.NotesMoved = n
	return rep, nil
}

// moveStateNotes relocates the free-form `notes:` block of novel-state.yaml into
// continuity/commits/book-notes.md and strips it from the state file.
//
// 那一段被写成 56 KB 的追加式工作日志：它是"我们这一轮做了什么"，属于 commits，
// 不是"进这一单元时世界是什么样"。混在状态文件里既拖慢解析，也误导读者。
func moveStateNotes(root string) (int, error) {
	statePath := filepath.Join(root, "novel-state.yaml")
	raw, err := os.ReadFile(statePath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	text := string(raw)

	const marker = "\nnotes:"
	idx := strings.Index(text, marker)
	if idx < 0 {
		if strings.HasPrefix(text, "notes:") {
			idx = 0
		} else {
			return 0, nil
		}
	}
	blockStart := idx
	if idx != 0 {
		blockStart = idx + 1 // 保留换行前的结构
	}
	// notes 是块标量：直到出现下一个顶层键（非缩进行）为止。
	rest := text[idx+1:]
	lines := strings.Split(rest, "\n")
	end := len(lines)
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			end = i
			break
		}
	}
	notesBlock := strings.Join(lines[:end], "\n")
	if strings.TrimSpace(notesBlock) == "" {
		return 0, nil
	}

	commitDir := filepath.Join(root, "continuity", "commits")
	if err := os.MkdirAll(commitDir, 0o755); err != nil {
		return 0, err
	}
	notesPath := filepath.Join(commitDir, "book-notes.md")
	var body strings.Builder
	body.WriteString("# 作者的卷批注与工作日志\n\n")
	body.WriteString("从 `novel-state.yaml` 的 `notes` 迁出。这是历史记录，不参与上下文构建。\n\n")
	body.WriteString(strings.TrimPrefix(notesBlock, "notes:"))
	body.WriteString("\n")
	if err := os.WriteFile(notesPath, []byte(body.String()), 0o644); err != nil {
		return 0, err
	}

	remaining := lines[end:]
	newText := text[:blockStart] + strings.Join(remaining, "\n")
	newText = strings.TrimRight(newText, "\n") + "\n"
	if err := os.WriteFile(statePath, []byte(newText), 0o644); err != nil {
		return 0, err
	}
	return len(notesBlock), nil
}

// StateSummary is the UI-facing digest of the continuity ledger.
type StateSummary struct {
	Units     int    `json:"units"`
	LastUnit  string `json:"lastUnit,omitempty"`
	AfterCh   int    `json:"afterCh,omitempty"`
	Cast      int    `json:"cast"`
	OpenLoops int    `json:"openLoops"`
}

// SummarizeState reports how far the ledger reaches, for the book header.
func SummarizeState(states []domain.UnitState) StateSummary {
	sum := StateSummary{Units: len(states)}
	if len(states) == 0 {
		return sum
	}
	last := states[len(states)-1]
	sum.LastUnit = last.Unit
	sum.AfterCh = last.AfterCh
	sum.Cast = len(last.Cast)
	for _, loop := range last.Loops {
		if !strings.EqualFold(loop.Status, "paid") {
			sum.OpenLoops++
		}
	}
	return sum
}

var _ = fmt.Sprintf
