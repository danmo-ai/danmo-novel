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

	// 结构不合规的总纲（缺必填根字段）必须被拒，且不能落盘。
	broken := strings.Replace(outlineWithUnknownReveal, "- logline:", "- logline_missing:", 1)
	err = writeBookFile(ctx, db, "write-test", "outline/book_outline.md", broken)
	if err == nil {
		t.Fatal("outline without logline must be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(root, "outline", "book_outline.md")); statErr == nil {
		t.Fatal("rejected outline must not be written to disk")
	}

	// 合规的总纲必须能写。
	if err := writeBookFile(ctx, db, "write-test", "outline/book_outline.md", outlineWithUnknownReveal); err != nil {
		t.Fatalf("valid outline must be accepted: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "outline", "book_outline.md"))
	if err != nil || !strings.Contains(string(raw), "全书总纲") {
		t.Fatalf("accepted outline missing on disk: err=%v raw=%q", err, string(raw))
	}
}
