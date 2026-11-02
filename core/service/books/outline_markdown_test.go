package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

const testBookMD = `# 全书总纲 · 测试
- schema_version: 1
- book_id: x
- title: 测试
- logline: 主角在困境中选择行动

## 故事线 · main
- kind: main
- description: 主线
- opening_state: 未开始
- ending_state: 已完成

## 分卷 · v01
- arc_id: arc-v01
- title: 第一卷
- chapter_range: 1-6
- purpose: 建立目标
- dramatic_question: 能否完成目标
- central_conflict: 目标与阻碍
- stakes: 失败代价
- entry_state: 未开始
- exit_state: 已完成
- thread_moves: main=未开始->已完成
- deliverables: 完成第一次行动
- open_questions: 下一步是什么
- reveal_ids: R1
- forbidden_reveal_ids: R2
- next_volume_entry: 继续行动

### 转折 · event-1
- event: 主角采取行动
- cause: 目标受到阻碍
- effect: 获得新的位置
- chapter_window: 1-6

## 备注
> note: 保留这条作者备注
`

func TestStructuredMarkdownFieldPatchPreservesSource(t *testing.T) {
	doc := ParseStructuredMarkdown(testBookMD, BookOutlineProtocol)
	if !doc.Valid() {
		t.Fatalf("valid document rejected: %+v", doc.Diagnostics)
	}
	arc, ok := doc.Entity("分卷", "v01")
	if !ok || arc.Value("deliverables") != "完成第一次行动" {
		t.Fatalf("arc not parsed: %+v", arc)
	}
	updated, err := ReplaceMarkdownField(testBookMD, BookOutlineProtocol, "分卷", "v01", "deliverables", "完成第一次行动并保住证据")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "> note: 保留这条作者备注") || !strings.Contains(updated, "完成第一次行动并保住证据") {
		t.Fatalf("minimal patch lost content:\n%s", updated)
	}
	if strings.Contains(updated, "- deliverables: 完成第一次行动\n") {
		t.Fatal("old field remained")
	}
}

func TestRevealScheduleParsesEntitiesAfterEmptyFields(t *testing.T) {
	doc := ParseStructuredMarkdown(sampleSettings()["canon/reveal-schedule.md"], "reveal-schedule/v1")
	if len(doc.EntitiesOf("揭示")) != 2 {
		t.Fatalf("reveal entities swallowed by empty field: %+v raw=%q", doc.Entities, sampleSettings()["canon/reveal-schedule.md"])
	}
}

func TestStructuredMarkdownMultilineFieldPatch(t *testing.T) {
	raw := testBookMD + "\n## 约束 · global\n- description: 第一行\n  第二行\n"
	updated, err := ReplaceMarkdownField(raw, BookOutlineProtocol, "约束", "global", "description", "新的第一行\n新的第二行")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "- description: 新的第一行\n  新的第二行") || !strings.Contains(updated, "> note: 保留这条作者备注") {
		t.Fatalf("multiline patch incorrect:\n%s", updated)
	}
	doc := ParseStructuredMarkdown(updated, BookOutlineProtocol)
	e, _ := doc.Entity("约束", "global")
	if e.Value("description") != "新的第一行\n新的第二行" {
		t.Fatalf("multiline value not preserved: %q", e.Value("description"))
	}
}

func TestStructuredMarkdownLegacyIsNotSilentlyValid(t *testing.T) {
	doc := ParseStructuredMarkdown("# 总纲\n\n## 分卷走向\n- v1 故事\n", BookOutlineProtocol)
	if !doc.Legacy || doc.Valid() {
		t.Fatalf("legacy must be read-only: %+v", doc)
	}
	if _, err := ReplaceMarkdownField("# 总纲\n", BookOutlineProtocol, "root", "", "title", "新书"); err == nil {
		t.Fatal("legacy local patch must be rejected")
	}
}

func TestStructuredMarkdownRejectsDuplicateEntity(t *testing.T) {
	doc := ParseStructuredMarkdown(testBookMD+"\n## 分卷 · v01\n- title: 重复\n", BookOutlineProtocol)
	if len(doc.Diagnostics) == 0 {
		t.Fatal("duplicate IDs must be diagnosed")
	}
}

func TestStructuredVolumeSeedInheritsUnitContract(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "structured-seed", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	seedTestIdentity(t, root)
	seedRevealSchedule(t, root)
	if err := writeTestFile(root, "outline/book_outline.md", testBookMD); err != nil {
		t.Fatal(err)
	}
	volume := `# 卷纲 · v01
- schema_version: 1
- volume_id: v01
- title: 第一卷
- source_arc_id: arc-v01
- chapter_range: 1-6
- goal: 能否完成目标
- central_conflict: 目标与阻碍
- stakes: 失败代价
- entry_state: 未开始
- exit_state: 已完成
- cast: lin

## 节拍 · event-1
- event: 主角申请复核
- cause: 旧材料存在疑点
- effect: 获得新的调查入口
- chapter_window: 1-6

## 单元 · v01-U1
- title: 起步
- chapter_range: 1-6
- objective: 获得行动资格
- obstacle: 尚未获得许可
- attempt: 查找旧材料
- setback: 材料不足
- choice: 按程序申请复核
- cost: 承担复核责任
- outcome: 获得三天复核许可
- entry_cause: v00-U9.next_hook
- exit_result: 获得许可
- state_changes: lin.role=新人->复核人
- clock_start_day: 1
- clock_end_day: 6
- clock_gap_days: 0
- timeline_id: main
- thread_ids: main
- event_ids: event-1
- reveal_ids: R1
- forbidden_reveal_ids: R2
- next_hook: 信息缺口 | 签名不一致
`
	cand := domain.Candidate{Mode: domain.PlanModeVolume, VolumeID: "v01", PlanMarkdown: map[string]string{"volume_outline": volume}}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	raw, err := readFile(root + "/outline/units/v01-U1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"source_seed_id: v01-U1", "desire: 获得行动资格", "choice: 按程序申请复核", "payoff: 获得三天复核许可", "out: 签名不一致", "field: role", "from: 新人", "to: 复核人"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("seed missing %q:\n%s", want, raw)
		}
	}
}

func TestStructuredSeedStateChangesReachFinalizationLedger(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U1", Volume: 1, OutlineRaw: "unit_id: v01-U1\nstory_day: 3\nstate_changes: lin.role_title=新人->复核人; lin.goal=申请->复核\nstate_deltas:\n  - stem: lin\n    field: role_title\n    from: 新人\n    to: 复核人\n  - stem: lin\n    field: goal\n    from: 申请\n    to: 复核\n"}
	state := StateFromOutline(unit, []domain.ChapterDraft{{Chapter: 3, Body: "正文"}})
	compact := StateFromOutline(domain.UnitInfo{UnitID: "v01-U1", Volume: 1, OutlineRaw: "story_day: 3\nend_day: 5\nstate_changes: lin.role_title=新人->复核人; lin.goal=申请->复核\n"}, []domain.ChapterDraft{{Chapter: 3, Body: "正文"}})
	if compact.Cast["lin"].Goal != "复核" || compact.EndDay != 5 {
		t.Fatalf("compact seed state was dropped: %+v", compact)
	}
	identified := StateFromOutline(unit, []domain.ChapterDraft{{Chapter: 3, Body: "正文"}}, []domain.IdentitySegment{{Stem: "lin", RoleTitle: "复核人", Org: "分局", FromUnit: "v01-U1"}})
	if identified.Cast["lin"].RoleTitle != "复核人" {
		t.Fatalf("identity timeline was not applied to state snapshot: %+v", identified)
	}
	if state.Cast["lin"].RoleTitle != "" || state.Cast["lin"].Goal != "复核" || state.StoryDay != 3 || state.EndDay != 3 {
		t.Fatalf("seed state did not reach continuity: %+v", state)
	}
}

func TestVolumeIdentityChangeIsAtomicAndEffective(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "identity-plan", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	seedTestIdentity(t, root)
	seedRevealSchedule(t, root)
	if err := writeTestFile(root, "outline/book_outline.md", testBookMD); err != nil {
		t.Fatal(err)
	}
	volume := strings.Replace(sampleVolumeOutline(), "## 单元 · v01-U2", `### 身份变更 · v01-U2-lin
- stem: lin
- role_title: 重案队试用
- org: 青江分局重案队
- from_unit: v01-U2
- reason: 调令生效

## 单元 · v01-U2`, 1)
	cand := domain.Candidate{Mode: domain.PlanModeVolume, VolumeID: "v01", PlanMarkdown: map[string]string{"volume_outline": volume}}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	segments, err := LoadIdentityTimeline(root)
	if err != nil || len(segments) != 2 {
		t.Fatalf("identity segments=%+v err=%v", segments, err)
	}
	if got := IdentityAt(segments, "lin", "v01-U2"); got == nil || got.RoleTitle != "重案队试用" {
		t.Fatalf("planned identity is not effective for next unit: %+v", got)
	}
	if segments[0].ToUnit != "v01-U1" {
		t.Fatalf("old segment was not closed: %+v", segments[0])
	}
}

func TestLegacyVolumeAcceptIsRejected(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "legacy-reject", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{Mode: domain.PlanModeVolume, VolumeID: "v01", PlanMarkdown: map[string]string{"volume_outline": "# 第一卷\n\n## 单元索引\n| unit_id | 章范围 | 功能 |\n|---|---|---|\n| v01-U1 | 1-3 | 试炼 |\n"}}
	if err := AcceptPlan(root, cand); err == nil || !strings.Contains(err.Error(), "迁移") {
		t.Fatalf("legacy production must require Agent migration: %v", err)
	}
}

func TestAcceptRejectsInvalidStructuredBook(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "invalid-book", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{Mode: domain.PlanModeInit, PlanMarkdown: map[string]string{"book_outline": "# 总纲\n- schema_version: 1\n- book_id: x\n", "cast_json": `[{"stem":"lin","name":"林晚","role":"protagonist","initialIdentity":{"roleTitle":"见习调查员","org":"青江分局","fromUnit":"v01-U1"}}]`}, PlanSettings: sampleSettings()}
	if err := AcceptPlan(root, cand); err == nil {
		t.Fatal("Accept must not bypass outline validation")
	}
}

func TestStructuredVolumeSeedAcceptKeepsExistingProse(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "structured", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	seedTestIdentity(t, root)
	seedRevealSchedule(t, root)
	if err := writeTestFile(root, "outline/book_outline.md", testBookMD); err != nil {
		t.Fatal(err)
	}
	volume := `# 卷纲 · v01
- schema_version: 1
- volume_id: v01
- title: 第一卷
- source_arc_id: arc-v01
- chapter_range: 1-6
- goal: 能否完成目标
- central_conflict: 目标与阻碍
- stakes: 失败代价
- entry_state: 未开始
- exit_state: 已完成
- cast: lin

## 节拍 · event-1
- event: 主角申请复核
- cause: 旧材料存在疑点
- effect: 获得新的调查入口
- chapter_window: 1-6

## 单元 · v01-U1
- title: 起步
- chapter_range: 1-3
- objective: 获得行动资格
- obstacle: 尚未获得许可
- attempt: 查找旧材料
- setback: 材料不足
- choice: 按程序申请复核
- cost: 承担复核责任
- outcome: 获得三天复核许可
- entry_cause: v00-U9.next_hook
- exit_result: 获得许可
- state_changes: lin.role=新人->复核人
- clock_start_day: 1
- clock_end_day: 6
- clock_gap_days: 0
- timeline_id: main
- thread_ids: main
- event_ids: event-1
- reveal_ids: R1
- forbidden_reveal_ids: R2
- next_hook: 信息缺口 | 签名不一致
`
	if err := writeTestFile(root, "outline/units/v01-U1.yaml", "unit_id: v01-U1\nstatus: drafted\ndesire: keep\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(root, "units/v01-U1.md", "正文保留\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(root, "outline/units/v01-U2.yaml", "unit_id: v01-U2\nstatus: drafted\n"); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{Mode: domain.PlanModeVolume, VolumeID: "v01", PlanMarkdown: map[string]string{"volume_outline": volume}}
	if err := AcceptPlan(root, cand); err == nil {
		t.Fatal("unit range gap must be rejected")
	}
	volume = strings.Replace(volume, "chapter_range: 1-3", "chapter_range: 1-6", 1)
	cand.PlanMarkdown["volume_outline"] = volume
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	prose, _ := readFile(root + "/units/v01-U1.md")
	if prose != "正文保留\n" {
		t.Fatal("accept volume touched prose")
	}
	old, _ := readFile(root + "/outline/units/v01-U1.yaml")
	if old != "unit_id: v01-U1\nstatus: drafted\ndesire: keep\n" {
		t.Fatal("existing unit contract was overwritten")
	}
}

func writeTestFile(root, rel, body string) error {
	path := root + "/" + rel
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func TestUnitContractForSeedKeepsInlineArraysClean(t *testing.T) {
	doc := ParseStructuredMarkdown(`# 卷纲 · v01
- schema_version: 1
- volume_id: v01
- title: 第一卷
- source_arc_id: arc-v01
- chapter_range: 1-6
- goal: 目标
- central_conflict: 冲突
- stakes: 代价
- entry_state: 开始
- exit_state: 结束
- cast: [lin, shen]

## 单元 · v01-U1
- title: 起步
- chapter_range: 1-6
- objective: 获得资格
- obstacle: 没有许可
- attempt: 找旧材料
- setback: 材料不足
- choice: 申请复核
- cost: 承担责任
- outcome: 拿到许可
- entry_cause: 报到
- exit_result: 获得许可
- state_changes: lin.goal=新人->复核人
- clock_start_day: 1
- clock_end_day: 6
- clock_gap_days: 0
- thread_ids: [career,relationship]
- reveal_ids: [R1]
- next_hook: 信息缺口 | 签名不一致
`, VolumeOutlineProtocol)
	seeds := SeedsFromStructuredVolume(doc)
	if len(seeds) != 1 {
		t.Fatalf("seeds=%d", len(seeds))
	}
	segments := []domain.IdentitySegment{
		{Stem: "lin", RoleTitle: "复核人", Org: "青江分局", FromUnit: "v01-U1"},
		{Stem: "shen", RoleTitle: "法医", Org: "市局", FromUnit: "v01-U2"},
	}
	c := UnitContractForSeed(seeds[0], splitList(doc.Root.Value("cast")), segments)
	threads, _ := c["thread_ids"].([]string)
	if strings.Join(threads, "|") != "career|relationship" {
		t.Fatalf("thread_ids must not keep brackets: %v", c["thread_ids"])
	}
	// shen 的身份从 v01-U2 才开始，不应被排进本单元场上。
	onStage, _ := c["on_stage"].([]string)
	if strings.Join(onStage, "|") != "lin" {
		t.Fatalf("on_stage 应只含本单元已有身份的角色: %v", c["on_stage"])
	}
	if c["pov"] != "lin" || c["unit_id"] != "v01-U1" || c["status"] != "proposed" {
		t.Fatalf("contract head wrong: %+v", c)
	}
}

// 尚未登场的角色不能因为出现在卷 cast 里，就被要求在每个单元都有身份段。
func TestUnitSeedSkipsCharactersWithoutIdentityYet(t *testing.T) {
	segments := []domain.IdentitySegment{
		{Stem: "lin", RoleTitle: "新警", Org: "分局", FromUnit: "v01-U1"},
		{Stem: "late", RoleTitle: "顾问", Org: "市局", FromUnit: "v01-U3"},
	}
	c := UnitContractForSeed(UnitSeed{UnitID: "v01-U1", From: 1, To: 3}, []string{"lin", "late"}, segments)
	onStage, _ := c["on_stage"].([]string)
	if strings.Join(onStage, "|") != "lin" {
		t.Fatalf("v01-U1 不应包含尚未登场的 late: %v", c["on_stage"])
	}
	c3 := UnitContractForSeed(UnitSeed{UnitID: "v01-U3", From: 7, To: 9}, []string{"lin", "late"}, segments)
	onStage3, _ := c3["on_stage"].([]string)
	if strings.Join(onStage3, "|") != "lin|late" {
		t.Fatalf("v01-U3 应包含已登场的 late: %v", c3["on_stage"])
	}
}

func TestSplitListStripsInlineArraySyntax(t *testing.T) {
	cases := map[string]string{
		"[career,relationship]":  "career|relationship",
		"[career, relationship]": "career|relationship",
		"career,relationship":    "career|relationship",
		"career；relationship":    "career|relationship",
		"[]":                     "",
		"[a]":                    "a",
	}
	for raw, want := range cases {
		if got := strings.Join(splitList(raw), "|"); got != want {
			t.Fatalf("splitList(%q)=%q want %q", raw, got, want)
		}
	}
}

func TestRevealIDListAcceptsAuthorPlaceholders(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"r01,r02", []string{"r01", "r02"}},
		{"r01、r02", []string{"r01", "r02"}},
		{"[r01,r02]", []string{"r01", "r02"}},
		{"【r01】", []string{"r01"}},
		{"r01 r02", []string{"r01", "r02"}},
		{"R1", []string{"R1"}},
		{"无", nil},
		{"无（第二部未解问题不得提前回答）", nil},
		{"没有（本卷先不锁）", nil},
		{"none", nil},
		{"[]", nil},
		{"", nil},
		{"r01,r01", []string{"r01"}},
	}
	for _, c := range cases {
		got := revealIDList(c.raw)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Fatalf("revealIDList(%q)=%v want %v", c.raw, got, c.want)
		}
	}
}

func TestAcceptPlanAcceptsEmptyRevealListWrittenAsProse(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "reveal-placeholder", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	seedTestIdentity(t, root)
	seedRevealSchedule(t, root)
	outline := strings.Replace(testBookMD, "- reveal_ids: R1", "- reveal_ids: 无（本卷没有可揭示内容）", 1)
	outline = strings.Replace(outline, "- forbidden_reveal_ids: R2", "- forbidden_reveal_ids: [R2]", 1)
	cand := domain.Candidate{Mode: domain.PlanModeInit, PlanMarkdown: map[string]string{"book_outline": outline, "cast_json": sampleCastJSON()}, PlanSettings: sampleSettings()}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatalf("placeholder reveal values must not block accept: %v", err)
	}
}

func TestAcceptPlanRejectsUnknownRevealID(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "reveal-unknown", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	seedTestIdentity(t, root)
	seedRevealSchedule(t, root)
	outline := strings.Replace(testBookMD, "- reveal_ids: R1", "- reveal_ids: R99", 1)
	cand := domain.Candidate{Mode: domain.PlanModeInit, PlanMarkdown: map[string]string{"book_outline": outline, "cast_json": sampleCastJSON()}, PlanSettings: sampleSettings()}
	err = AcceptPlan(root, cand)
	if err == nil || !strings.Contains(err.Error(), "v01.reveal_ids") || !strings.Contains(err.Error(), "R99") {
		t.Fatalf("unknown reveal id must name the field and value: %v", err)
	}
}

func TestAcceptPlanRejectsProseInRevealField(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "reveal-prose", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	seedTestIdentity(t, root)
	seedRevealSchedule(t, root)
	outline := strings.Replace(testBookMD, "- reveal_ids: R1", "- reveal_ids: R1、本卷暂不揭示", 1)
	cand := domain.Candidate{Mode: domain.PlanModeInit, PlanMarkdown: map[string]string{"book_outline": outline, "cast_json": sampleCastJSON()}, PlanSettings: sampleSettings()}
	err = AcceptPlan(root, cand)
	if err == nil || !strings.Contains(err.Error(), "只接受揭示 ID 列表") {
		t.Fatalf("prose in reveal field must be actionable: %v", err)
	}
}

func sampleCastJSON() string {
	return `[{"stem":"lin","name":"林晚","role":"protagonist","cardMarkdown":"# 林晚\n","initialIdentity":{"roleTitle":"见习调查员","org":"青江分局","fromUnit":"v01-U1"}}]`
}
