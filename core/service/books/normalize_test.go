package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeUnit(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 旧字段名必须被改名，而不是留到运行期等报错。
func TestNormalizeBookDataRenamesLegacyFields(t *testing.T) {
	root := t.TempDir()
	body := `unit_id: v01-U1
chapter_range: [1, 3]
state_deltas:
  - {stem: lin, field: injury, from: "受伤", to: "已愈"}
  - {stem: lin, field: position, from: "外门", to: "内门"}
  - {stem: lin, field: knowledge, from: "", to: "知道钥匙"}
  - {stem: lin, field: identity, from: "新人", to: "复核人"}
`
	writeUnit(t, root, "outline/units/v01-U1.yaml", body)

	rep, err := NormalizeBookData(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "outline/units/v01-U1.yaml"))
	got := string(raw)
	for _, want := range []string{"field: health", "field: location", "field: knows", "field: role_title"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if rep.FieldsRenamed != 4 {
		t.Fatalf("renamed=%d want 4", rep.FieldsRenamed)
	}
}

// status 是案件进展时不能映射成 life，只能删掉；死亡语义才映射。
func TestNormalizeBookDataKeepsVolumePlanningNotes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "outline", "volumes")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# 卷纲 · v01\n- schema_version: 1\n\n## 单元 · v01-U1\n- title: 起步\n- state_changes: lin.knowledge=旧->新；lin.evidence=零散->入账\n"
	if err := os.WriteFile(filepath.Join(path, "v01.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeBookData(root); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(path, "v01.md"))
	got := string(raw)
	if !strings.Contains(got, "lin.knows=旧->新") {
		t.Fatalf("known alias must be renamed:\n%s", got)
	}
	if !strings.Contains(got, "lin.evidence=零散->入账") {
		t.Fatalf("non-contract planning note must be preserved:\n%s", got)
	}
}

func TestNormalizeBookDataSplitsStatusSemantics(t *testing.T) {
	root := t.TempDir()
	body := `unit_id: v01-U1
chapter_range: [1, 3]
state_deltas:
  - {stem: boss, field: status, from: "在案", to: "落网"}
  - {stem: shiye, field: status, from: "在案智囊", to: "病故"}
`
	writeUnit(t, root, "outline/units/v01-U1.yaml", body)

	rep, err := NormalizeBookData(root)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "outline/units/v01-U1.yaml"))
	got := string(raw)
	if strings.Contains(got, "落网") {
		t.Fatalf("案件进展 status 必须被删除：\n%s", got)
	}
	if !strings.Contains(got, "field: life") || !strings.Contains(got, "病故") {
		t.Fatalf("死亡语义必须映射成 life：\n%s", got)
	}
	if rep.FieldsDropped["status"] != 1 {
		t.Fatalf("dropped=%v", rep.FieldsDropped)
	}
}

// 卷纲种子的紧凑写法是主要数据来源，必须一起规整。
func TestNormalizeBookDataRenamesCompactChanges(t *testing.T) {
	root := t.TempDir()
	body := `unit_id: v01-U1
chapter_range: [1, 3]
state_changes: lin.injury=受伤->已愈；lin.method=旧法->新法；career.access=无->协查
`
	writeUnit(t, root, "outline/units/v01-U1.yaml", body)

	if _, err := NormalizeBookData(root); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "outline/units/v01-U1.yaml"))
	got := string(raw)
	if !strings.Contains(got, "lin.health=受伤->已愈") {
		t.Fatalf("紧凑写法未改名：\n%s", got)
	}
	if strings.Contains(got, "lin.method") || strings.Contains(got, "career.access") {
		t.Fatalf("不可映射的项必须删除：\n%s", got)
	}
}

// word_target 折算成 chapter_chars 后删除：代码已不再读它。
func TestNormalizeBookDataConvertsWordTarget(t *testing.T) {
	root := t.TempDir()
	body := `unit_id: v01-U1
chapter_range: [1, 6]
word_target: 30000
`
	writeUnit(t, root, "outline/units/v01-U1.yaml", body)

	if _, err := NormalizeBookData(root); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "outline/units/v01-U1.yaml"))
	got := string(raw)
	if strings.Contains(got, "word_target") {
		t.Fatalf("word_target 必须删除：\n%s", got)
	}
	// 30000 / 6 章 = 5000 → 夹到上限 3500。
	if !strings.Contains(got, "chapter_chars: 3500") {
		t.Fatalf("chapter_chars 折算错误：\n%s", got)
	}
}

// 已有 chapter_chars 时只删 word_target，不改数字。
func TestNormalizeBookDataDropsWordTargetWhenCharsPresent(t *testing.T) {
	root := t.TempDir()
	body := `unit_id: v01-U1
chapter_range: [1, 6]
word_target: 30000
chapter_chars: 2500
`
	writeUnit(t, root, "outline/units/v01-U1.yaml", body)

	if _, err := NormalizeBookData(root); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "outline/units/v01-U1.yaml"))
	got := string(raw)
	if strings.Contains(got, "word_target") || !strings.Contains(got, "chapter_chars: 2500") {
		t.Fatalf("应保留原 chapter_chars：\n%s", got)
	}
}

// 连续账本的 injury 键改名，且原文留档。
func TestNormalizeBookDataRenamesLedgerInjury(t *testing.T) {
	root := t.TempDir()
	writeUnit(t, root, StatePath, "version: 1\nunits:\n  - unit: v01-U1\n    cast:\n      lin:\n        injury: 刀伤\n")
	rep, err := NormalizeBookData(root)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.StateRepaired {
		t.Fatal("state ledger should be repaired")
	}
	raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(StatePath)))
	if strings.Contains(string(raw), "injury:") || !strings.Contains(string(raw), "health:") {
		t.Fatalf("ledger not renamed:\n%s", raw)
	}
}

// 旧揭示计划删除前必须留档——它是唯一一份原文。
func TestNormalizeBookDataArchivesLegacyReveal(t *testing.T) {
	root := t.TempDir()
	writeUnit(t, root, "canon/reveal-schedule.md", "# 揭示计划\n自由表格\n")
	rep, err := NormalizeBookData(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "canon/reveal-schedule.md")); err == nil {
		t.Fatal("legacy reveal must be removed from canon")
	}
	if len(rep.FilesRemoved) != 1 {
		t.Fatalf("removed=%v", rep.FilesRemoved)
	}
	// 备份必须存在，否则「退役旧文件」等于删数据。
	var found bool
	_ = filepath.Walk(filepath.Join(root, "backups"), func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, "reveal-schedule.md") {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("removed file must be archived under backups/")
	}
}

// 登记表里的旧状态与位置冲突要就地修掉，否则迁移完仍然校验不过。
func TestRepairForeshadowConflicts(t *testing.T) {
	root := t.TempDir()
	writeUnit(t, root, ForeshadowSchedulePath, `# 伏笔登记表
- schema_version: 1

## 伏笔 · FS-01
- content: 甲
- type: 物件
- plant_volume: v03
- payoff_volume: v01
- status: locked

## 伏笔 · FS-02
- content: 乙
- type: 物件
- plant_volume: v01
- plant_unit: v01-U2
- plant_chapter: 8
- payoff_volume: v01
- payoff_unit: v01-U2
- payoff_chapter: 5
- status: planned
`)
	notes, err := repairForeshadowConflicts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) < 2 {
		t.Fatalf("expected at least two repairs, got %v", notes)
	}
	raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(ForeshadowSchedulePath)))
	if err := ValidateForeshadowSchedule(string(raw)); err != nil {
		t.Fatalf("after repair the registry must validate: %v\n%s", err, raw)
	}
}
