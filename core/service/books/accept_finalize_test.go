package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func setupFinalizeBook(t *testing.T) (string, domain.UnitInfo) {
	t.Helper()
	root, err := CreateBookScaffold(t.TempDir(), "fin-test", "测试书", "玄幻")
	if err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(root, "outline", "units")
	yaml := `unit_id: v01-U1
status: drafted
title_working: "入门试炼"
function: "觉醒与试炼入门"
chapter_range: [1, 2]
on_stage: ["linchen", "wangmou"]
pov: "linchen"
next_hook:
  type: 危机
state_deltas:
  - {stem: linchen, field: location, from: "外门", to: "山门"}
  - {stem: linchen, field: goal, from: "活下去", to: "入内门"}
  - {stem: wangmou, field: location, from: "账房", to: "演武场"}
`
	if err := os.WriteFile(filepath.Join(unitDir, "v01-U1.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	// scaffold 不再生成 facts.md（该文件已退役）；这里显式造一个旧文件，
	// 用来验证迁移会删掉它，也验证"名字相近但不是它"的文件不会被误删。
	if err := os.WriteFile(filepath.Join(root, "continuity", "facts.md"), []byte("# 旧账本\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "continuity", "legacy-facts.md"), []byte("# legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	return root, snap.Units[0]
}

// 定稿接受必须真正更新连续性状态 / 卷摘要 / 定稿账本。
//
// 旧实现维护 continuity/facts.md（游标 + 人物快照表）；那份文件会被逐轮追加成
// 散文日志，游标自相矛盾、人物状态段从不合并。现在状态改由
// continuity/state.yaml 的按单元段表达，facts.md 不再读写。
func TestApplyFinalizationLedger(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。他被点名上场。"},
		{Chapter: 2, Title: "爆发", Body: "名额只剩一个。他燃烧代价换来一次爆发。"},
	}
	actual := []domain.ChapterActual{{Chapter: 1, Summary: "林尘被点名上场"}, {Chapter: 2, Summary: "林尘付代价完成试炼", StateDeltas: []domain.ChapterActualDelta{{Stem: "linchen", Field: "location", From: "外门", To: "山门", Evidence: "他燃烧代价换来一次爆发", Confirmed: true}}}}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatal(err)
	}

	// 1. 状态推进
	raw, _ := os.ReadFile(filepath.Join(root, "outline", "units", "v01-U1.yaml"))
	if !strings.Contains(string(raw), "status: finalized") {
		t.Fatalf("unit status not finalized: %s", raw)
	}

	// 2. 连续性状态落段：单元号 + 定稿章号 + 人物当前状态
	states, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Unit != "v01-U1" {
		t.Fatalf("state segments=%+v", states)
	}
	if states[0].AfterCh != 2 {
		t.Fatalf("afterCh=%d want last finalized chapter 2", states[0].AfterCh)
	}
	if len(states[0].Cast) == 0 {
		t.Fatalf("no character state recorded: %+v", states[0])
	}

	// 3. facts.md 不再被写回路径维护。脚手架可能创建了它，但定稿不写。
	// 用内容比对确认它未被改动：游标/人物快照已迁到 state.yaml。
	factsPath := filepath.Join(root, "continuity", "facts.md")
	if raw, err := os.ReadFile(factsPath); err == nil {
		if strings.Contains(string(raw), "Cast snapshot") || strings.Contains(string(raw), "last_committed_ch") {
			t.Fatalf("facts.md must not be used as the state ledger anymore:\n%s", raw)
		}
	}

	// 4. 卷摘要
	sum, err := os.ReadFile(filepath.Join(root, "continuity", "summaries", "v01.md"))
	if err != nil {
		t.Fatalf("summary missing: %v", err)
	}
	if !strings.Contains(string(sum), "| 1 |") || !strings.Contains(string(sum), "| 2 |") {
		t.Fatalf("summary missing chapters: %s", sum)
	}

	// 5. 定稿账本
	commit, err := os.ReadFile(filepath.Join(root, "continuity", "commits", "v01-U1.md"))
	if err != nil {
		t.Fatalf("commit log missing: %v", err)
	}
	if !strings.Contains(string(commit), "总字数") {
		t.Fatalf("commit log incomplete: %s", commit)
	}
}
