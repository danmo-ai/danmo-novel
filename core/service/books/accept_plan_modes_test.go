package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func sampleSettings() map[string]string {
	out := map[string]string{}
	for _, p := range PlanSettingsPaths {
		out[p] = "# " + p + "\ncontent\n"
	}
	out["canon/locked-terms.yaml"] = "locked_until: {}\ncompliance: []\naliases: {}\n"
	out["canon/reveal-schedule.md"] = "# 揭示计划\n- schema_version: 1\n\n## 揭示 · R1\n- public_summary: 可公开信息\n- earliest_volume: v01\n- hint_unit_ids: v01-U1\n- confirm_unit_id: v01-U2\n- status: locked\n- locked_terms: 无\n\n## 揭示 · R2\n- public_summary: 暂缓公开信息\n- earliest_volume: v02\n- hint_unit_ids: v02-U1\n- confirm_unit_id: v02-U2\n- status: locked\n- locked_terms: 无\n"
	return out
}

// seedRevealSchedule registers the reveal IDs the sample outlines reference.
// 卷纲与总纲现在走同一套引用校验，所以任何引用 R1/R2 的 fixture 都必须先
// 有一份真的揭示计划；scaffold 默认是空计划，属于"尚未登记"而不是"存在 R1"。
func seedRevealSchedule(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "canon", "reveal-schedule.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(sampleSettings()["canon/reveal-schedule.md"]), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sampleBookOutline() string {
	return `# 全书总纲 · 测试
- schema_version: 1
- book_id: test
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
`
}

func sampleVolumeOutline() string {
	return `# 卷纲 · v01
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
- event: 主角采取行动
- cause: 目标受到阻碍
- effect: 获得新的位置
- chapter_window: 1-6

## 单元 · v01-U1
- title: 旧邮袋
- chapter_range: 1-3
- objective: 获得行动资格
- obstacle: 尚未获得许可
- attempt: 查找旧材料
- setback: 材料不足
- choice: 按程序申请复核
- cost: 承担复核责任
- outcome: 获得三天复核许可
- entry_cause: 开卷目标受阻
- exit_result: 获得许可
- state_changes: lin.role=新人->复核人
- clock_start_day: 1
- clock_end_day: 3
- clock_gap_days: 0
- timeline_id: main
- thread_ids: main
- event_ids: event-1
- reveal_ids: R1
- forbidden_reveal_ids: R2
- next_hook: 信息缺口 | 签名不一致

## 单元 · v01-U2
- title: 旧识试探
- chapter_range: 4-6
- objective: 核实材料来源
- obstacle: 旧识拒绝合作
- attempt: 比对旧记录
- setback: 记录相互矛盾
- choice: 承担压力继续核查
- cost: 暴露调查方向
- outcome: 固定第一份实证
- entry_cause: v01-U1.next_hook
- exit_result: 获得实证
- state_changes: lin.evidence=疑点->实证
- clock_start_day: 4
- clock_end_day: 6
- clock_gap_days: 1
- timeline_id: main
- thread_ids: main
- event_ids: event-1
- reveal_ids: R1
- forbidden_reveal_ids: R2
- next_hook: 信息缺口 | 实证指向旧识
`
}

func TestAcceptPlanInitWritesSettingsAndCastOnly(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "init-test", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		Mode: domain.PlanModeInit,
		PlanMarkdown: map[string]string{
			"book_outline": sampleBookOutline(),
			"cast_json":    `[{"stem":"lin","name":"林晚","role":"protagonist","initialIdentity":{"roleTitle":"见习调查员","org":"青江分局","fromUnit":"v01-U1","note":"初始身份"},"tagline":"痕迹检验","cardMarkdown":"# 林晚\n"}]`,
		},
		PlanSettings: sampleSettings(),
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	for _, p := range PlanSettingsPaths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
			t.Fatalf("missing setting %s: %v", p, err)
		}
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !HasBookOutline(snap) {
		t.Fatal("expected book outline")
	}
	if len(snap.Volumes) != 0 {
		t.Fatalf("init must not write volumes: %d", len(snap.Volumes))
	}
	if len(snap.Units) != 0 {
		t.Fatalf("init must not seed units: %d", len(snap.Units))
	}
	if len(snap.Cast) != 1 || snap.Cast[0].Status != domain.CastCanon {
		t.Fatalf("cast=%+v", snap.Cast)
	}
	kind, _, vol := SelectPrimaryAction(snap)
	if kind != domain.TaskPlan || vol != 1 {
		t.Fatalf("next after init should be volume plan, got %s vol=%d", kind, vol)
	}
}

func TestAcceptPlanReinitPreservesVolumeUnitsProse(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "reinit-test", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	volPath := filepath.Join(root, "outline", "volumes", "v01.md")
	if err := os.MkdirAll(filepath.Dir(volPath), 0o755); err != nil {
		t.Fatal(err)
	}
	volBody := "# 保留卷纲\n"
	if err := os.WriteFile(volPath, []byte(volBody), 0o644); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(root, "outline", "units", "v01-U1.yaml")
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "unit_id: v01-U1\nstatus: drafted\ndesire: keep-me\n"
	if err := os.WriteFile(unitPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	prosePath := filepath.Join(root, "units", "v01-U1.md")
	if err := os.MkdirAll(filepath.Dir(prosePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prosePath, []byte("正文保留\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	factsPath := filepath.Join(root, "continuity", "facts.md")
	if err := os.MkdirAll(filepath.Dir(factsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(factsPath, []byte("# facts keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cand := domain.Candidate{
		Mode: domain.PlanModeReinit,
		PlanMarkdown: map[string]string{
			"book_outline": sampleBookOutline(),
			"cast_json":    `[{"stem":"lin","name":"林晚","role":"protagonist","initialIdentity":{"roleTitle":"见习调查员","org":"青江分局","fromUnit":"v01-U1","note":"初始身份"},"cardMarkdown":"# 林晚\n"}]`,
		},
		PlanSettings: sampleSettings(),
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("existing unit overwritten:\n%s", got)
	}
	prose, err := os.ReadFile(prosePath)
	if err != nil || !strings.Contains(string(prose), "正文保留") {
		t.Fatalf("prose touched: %s %v", prose, err)
	}
	vol, err := os.ReadFile(volPath)
	if err != nil || string(vol) != volBody {
		t.Fatalf("volume touched: %s %v", vol, err)
	}
	facts, err := os.ReadFile(factsPath)
	if err != nil || !strings.Contains(string(facts), "facts keep") {
		t.Fatalf("continuity touched: %s %v", facts, err)
	}
	if _, err := os.Stat(filepath.Join(root, "outline", "units", "v01-U2.yaml")); err == nil {
		t.Fatal("reinit must not seed units")
	}
	bible, _ := os.ReadFile(filepath.Join(root, "book-bible.md"))
	if !strings.Contains(string(bible), "book-bible.md") {
		t.Fatalf("settings not rewritten: %s", bible)
	}
}

func TestAcceptPlanReinitPreservePathsKeepsFiles(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "reinit-preserve", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "canon", "cast"), 0o755); err != nil {
		t.Fatal(err)
	}
	castPath := filepath.Join(root, "canon", "cast", "lin.md")
	refined := "# 林晚\n`status`: canon\n`role`: protagonist\n精修版\n"
	if err := os.WriteFile(castPath, []byte(refined), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outline", "book_outline.md"), []byte("# 旧总纲\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		Mode:          domain.PlanModeReinit,
		PreservePaths: []string{"canon/cast/lin.md", "canon/world.md"},
		PlanMarkdown: map[string]string{
			"book_outline": sampleBookOutline(),
			"cast_json":    `[{"stem":"lin","name":"林晚","role":"protagonist","initialIdentity":{"roleTitle":"见习调查员","org":"青江分局","fromUnit":"v01-U1","note":"初始身份"},"cardMarkdown":"# 林晚 覆盖版\n"}]`,
		},
		PlanSettings: sampleSettings(),
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(castPath)
	if string(got) != refined {
		t.Fatalf("preserved cast overwritten: %s", got)
	}
	world, _ := os.ReadFile(filepath.Join(root, "canon", "world.md"))
	if strings.Contains(string(world), "# canon/world.md\ncontent") {
		t.Fatalf("preserved world overwritten: %s", world)
	}
	book, _ := os.ReadFile(filepath.Join(root, "outline", "book_outline.md"))
	if !strings.Contains(string(book), "全书总纲") {
		t.Fatalf("book outline should be replaced: %s", book)
	}
}

func seedTestIdentity(t *testing.T, root string) {
	t.Helper()
	if err := SaveIdentityTimeline(root, []domain.IdentitySegment{{Stem: "lin", RoleTitle: "见习调查员", Org: "青江分局", FromUnit: "v01-U1"}}); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptPlanVolumeDoesNotWriteSettingsCastOrBook(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "vol-test", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	seedTestIdentity(t, root)
	seedRevealSchedule(t, root)
	if err := os.WriteFile(filepath.Join(root, "outline", "book_outline.md"), []byte(sampleBookOutline()), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "book-bible.md"))
	if err != nil {
		t.Fatal(err)
	}
	cand := domain.Candidate{
		Mode: domain.PlanModeVolume, VolumeID: "v01",
		PlanMarkdown: map[string]string{
			"book_outline":   "# 应忽略的总纲\n",
			"volume_outline": sampleVolumeOutline(),
			"cast_json":      `[{"stem":"lin","name":"林晚","role":"protagonist","initialIdentity":{"roleTitle":"见习调查员","org":"青江分局","fromUnit":"v01-U1","note":"初始身份"},"cardMarkdown":"# 林晚\n"}]`,
		},
		PlanSettings: map[string]string{"book-bible.md": "# hacked\n"},
	}
	if err := AcceptPlan(root, cand); err == nil {
		t.Fatal("expected reject when volume mode carries settings")
	}
	cand.PlanSettings = nil
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, "book-bible.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("volume mode must not rewrite settings")
	}
	raw, _ := os.ReadFile(filepath.Join(root, "outline", "book_outline.md"))
	if strings.Contains(string(raw), "应忽略的总纲") {
		t.Fatal("volume mode must not write book outline")
	}
	if _, err := os.Stat(filepath.Join(root, "canon", "cast", "lin.md")); err == nil {
		t.Fatal("volume mode must not write cast")
	}
	if _, err := os.Stat(filepath.Join(root, "outline", "volumes", "v01.md")); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Units) != 2 {
		t.Fatalf("units=%d", len(snap.Units))
	}
}
