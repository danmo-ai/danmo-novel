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
  paid: [FS-01]
  position: 山门
`
	if err := os.WriteFile(filepath.Join(unitDir, "v01-U1.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	facts := `# Continuity facts

## Public facts
| Kind | Fact | First seen | Notes |
|------|------|------------|-------|

## Tracking
### Cursor
- last_committed_ch: 0

### Cast snapshot
| 角色 | 年龄 | 职位 | 位置 | 目标 | 伤势/资源 | 知情范围 | 关系质态 |
|------|------|------|------|------|-----------|----------|----------|
| linchen | 16 | 外门弟子 | 山门 | 入内门 | 无 | 不知账本 | 独来独往 |

## Open loops
| FS-id | 内容 | 状态 | 埋点章 | 计划回收 | 实际回收 |
|-------|------|------|--------|----------|----------|
| FS-01 | 刀柄来历 | planted | 1 | 5 | |
| FS-02 | 账房亏空 | planted | 2 | 6 | |
`
	if err := os.WriteFile(filepath.Join(root, "continuity", "facts.md"), []byte(facts), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	return root, snap.Units[0]
}

// P0-3: 定稿接受必须真正更新 facts.md / Cast snapshot / Open loops / 卷摘要 / 定稿账本。
func TestApplyFinalizationLedger(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。他被点名上场。"},
		{Chapter: 2, Title: "爆发", Body: "名额只剩一个。他燃烧代价换来一次爆发。"},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, nil); err != nil {
		t.Fatal(err)
	}

	// 1. 状态推进
	raw, _ := os.ReadFile(filepath.Join(root, "outline", "units", "v01-U1.yaml"))
	if !strings.Contains(string(raw), "status: reviewed") {
		t.Fatalf("unit status not reviewed: %s", raw)
	}

	// 2. facts.md 游标 + 伏笔推进 + Cast snapshot 新增 wangmou
	facts, _ := os.ReadFile(filepath.Join(root, "continuity", "facts.md"))
	text := string(facts)
	if !strings.Contains(text, "last_committed_ch: 2") {
		t.Fatalf("cursor not updated: %s", text)
	}
	if !strings.Contains(text, "| FS-01 | 刀柄来历 | paid") {
		t.Fatalf("FS-01 not advanced: %s", text)
	}
	if strings.Contains(text, "| FS-02 | 账房亏空 | paid") {
		t.Fatalf("FS-02 wrongly advanced: %s", text)
	}
	if !strings.Contains(text, "| wangmou |") {
		t.Fatalf("cast snapshot missing on-stage character: %s", text)
	}

	// 3. 卷摘要
	sum, err := os.ReadFile(filepath.Join(root, "continuity", "summaries", "v01.md"))
	if err != nil {
		t.Fatalf("summary missing: %v", err)
	}
	if !strings.Contains(string(sum), "| 1 |") || !strings.Contains(string(sum), "| 2 |") {
		t.Fatalf("summary missing chapters: %s", sum)
	}

	// 4. 定稿账本
	commit, err := os.ReadFile(filepath.Join(root, "continuity", "commits", "v01-U1.md"))
	if err != nil {
		t.Fatalf("commit log missing: %v", err)
	}
	if !strings.Contains(string(commit), "总字数") || !strings.Contains(string(commit), "危机") {
		t.Fatalf("commit log incomplete: %s", commit)
	}
}
