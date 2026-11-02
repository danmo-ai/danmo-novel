package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

const outlineWithUnknownReveal = `# 全书总纲 · 测试
- schema_version: 1
- book_id: write-test
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
- reveal_ids: r99
- forbidden_reveal_ids:
- next_volume_entry: 继续行动
`

func TestWriteBookFileValidatesStructuredOutline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	root, err := books.CreateBookScaffold(dir, "write-test", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(ctx, "write-test", root, "测试", "rev-1", false); err != nil {
		t.Fatal(err)
	}

	// 没有揭示计划里不存在的 ID 时，写入必须被拒，且不能落盘。
	err = writeBookFile(ctx, db, "write-test", "outline/book_outline.md", outlineWithUnknownReveal)
	if err == nil || !strings.Contains(err.Error(), "reveal_ids") {
		t.Fatalf("unknown reveal id must be rejected: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "outline", "book_outline.md")); statErr == nil {
		t.Fatal("rejected outline must not be written to disk")
	}

	// 空值行（"无" 的自然写法）属于合规，必须能写。
	valid := strings.Replace(outlineWithUnknownReveal, "reveal_ids: r99", "reveal_ids: 无（第二部未解问题不得提前回答）", 1)
	if err := writeBookFile(ctx, db, "write-test", "outline/book_outline.md", valid); err != nil {
		t.Fatalf("placeholder reveal value must be accepted: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "outline", "book_outline.md"))
	if err != nil || !strings.Contains(string(raw), "第二部未解问题") {
		t.Fatalf("accepted outline missing on disk: err=%v raw=%q", err, string(raw))
	}
}
