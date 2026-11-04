package contextsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// 久未更新的字段必须在提示词里被标出来，否则模型会把它当成此刻的事实。
// 实测一条 63 天前的伤势被当作「当前身体状况」写进了写作包。
func TestStaleFieldIsAnnotatedInPrompt(t *testing.T) {
	root := t.TempDir()
	// 账本每段都是全量快照（生产由 RebuildStateFrom 保证）。
	writeLedger(t, root, `version: 1
units:
    - unit: v01-U1
      volume: 1
      after_ch: 3
      cast:
        lin:
            health: 刀伤
    - unit: v01-U2
      volume: 1
      after_ch: 6
      cast:
        lin:
            health: 刀伤
    - unit: v01-U3
      volume: 1
      after_ch: 9
      cast:
        lin:
            health: 刀伤
    - unit: v01-U4
      volume: 1
      after_ch: 12
      cast:
        lin:
            health: 刀伤
    - unit: v01-U5
      volume: 1
      after_ch: 15
      cast:
        lin:
            health: 刀伤
`)
	units := []domain.UnitInfo{}
	for _, id := range []string{"v01-U1", "v01-U2", "v01-U3", "v01-U4", "v01-U5"} {
		raw := "unit_id: " + id + "\n"
		if id == "v01-U1" {
			raw += "state_deltas:\n  - {stem: lin, field: health, from: x, to: 刀伤}\n"
		}
		units = append(units, domain.UnitInfo{UnitID: id, Volume: 1, OutlineRaw: raw})
	}
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b", RootPath: root},
		Units:   units,
	}
	// U5 读 U4 的快照；health 最后写入于 U1，已过 3 个单元，达到阈值。
	got := ExtractContinuity(snap, domain.UnitInfo{UnitID: "v01-U5"}, []string{"lin"}, ContinuityForWrite)
	if !strings.Contains(got, "可能已变") {
		t.Fatalf("陈旧字段必须被标注：\n%s", got)
	}
	if !strings.Contains(got, "刀伤") {
		t.Fatalf("值本身仍要给出：\n%s", got)
	}
}

// 刚写入的字段不该带噪音标注。
func TestFreshFieldIsNotAnnotated(t *testing.T) {
	root := t.TempDir()
	writeLedger(t, root, `version: 1
units:
    - unit: v01-U1
      volume: 1
      after_ch: 3
      cast:
        lin:
            health: 刀伤
    - unit: v01-U2
      volume: 1
      after_ch: 6
`)
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b", RootPath: root},
		Units: []domain.UnitInfo{
			{UnitID: "v01-U1", Volume: 1, OutlineRaw: "unit_id: v01-U1\nstate_deltas:\n  - {stem: lin, field: health, from: x, to: 刀伤}\n"},
			{UnitID: "v01-U2", Volume: 1, OutlineRaw: "unit_id: v01-U2\n"},
		},
	}
	got := ExtractContinuity(snap, domain.UnitInfo{UnitID: "v01-U2"}, []string{"lin"}, ContinuityForWrite)
	if strings.Contains(got, "可能已变") {
		t.Fatalf("相邻单元不该报警：\n%s", got)
	}
}

func writeLedger(t *testing.T, root, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(books.StatePath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
