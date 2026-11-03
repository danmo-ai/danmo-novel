package contextsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

func TestExtractVolumeHandoffKeepsBridgeOnly(t *testing.T) {
	raw := `# 第一卷 · 旧邮袋

## 本卷目标
- 撕开账房裂缝

## 本卷人物
- linchen
- wangmou

## 单元索引
| unit_id | 章范围 | 一句话功能 | 终局边界 | 下一钩类型 |
|---|---|---|---|---|
| v01-U1 | 1-3 | 旧邮袋 | 不揭真相 | 危机 |
| v01-U2 | 4-6 | 旧识试探 | 不合作 | 身份 |
| v01-U3 | 7-9 | 账房夜闯 | 不抓现行 | 悬念 |
| v01-U4 | 10-12 | 山门对峙 | 身份半露 | 危机 |
| v01-U5 | 13-15 | 卷末一击 | 留下钩子 | 身份 |
| v01-U6 | 16-18 | 多余行 | 不应出现在末段前 | 危机 |

## 卷末状态
- 林尘暂离山门
- 账房亏空未结

## 冗长附录
` + strings.Repeat("很长的附录正文。", 80)

	got := ExtractVolumeHandoff(raw)
	if got == "" {
		t.Fatal("empty handoff")
	}
	if !strings.Contains(got, "林尘暂离山门") || !strings.Contains(got, "linchen") {
		t.Fatalf("missing bridge fields: %s", got)
	}
	if strings.Contains(got, "冗长附录") || strings.Contains(got, "v01-U1") {
		t.Fatalf("should keep tail units only, not whole file: %s", got)
	}
	if !strings.Contains(got, "v01-U6") || !strings.Contains(got, "v01-U2") {
		t.Fatalf("expected last 5 unit rows: %s", got)
	}
	if len([]rune(got)) > 2500 {
		t.Fatalf("handoff too large: %d", len([]rune(got)))
	}
}

// 卷纲要的"已揭/未揭"来自状态账，而不是旧的散文账本：
// 已回收的伏笔不再注入（它们是历史，不是约束）。
func TestContinuityLatestSkipsPaidLoops(t *testing.T) {
	root := t.TempDir() + "/book"
	src := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	if err := copyTree(src, root); err != nil {
		t.Fatal(err)
	}
	state := `version: 1
units:
  - unit: v01-U1
    volume: 1
    after_ch: 3
    cast:
      linchen: {location: 山门外, goal: 查账}
    loops:
      - {id: FS-01, status: planted, detail: 刀柄来历}
      - {id: FS-02, status: advanced, detail: 账房亏空}
      - {id: FS-03, status: paid, detail: 旧债}
`
	if err := os.WriteFile(filepath.Join(root, books.StatePath), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractContinuityLatest(snap, nil, ContinuityForReview)
	if !strings.Contains(got, "linchen") || !strings.Contains(got, "FS-01") {
		t.Fatalf("missing state: %s", got)
	}
	if strings.Contains(got, "FS-03") {
		t.Fatalf("paid loops must be skipped: %s", got)
	}
}

func TestBuildPlanPacketVolumeIncludesPrevHandoff(t *testing.T) {
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo", Genre: "玄幻"},
		Files: map[string]string{
			"outline/book_outline.md": "# 总纲\n分两卷\n",
			"book-bible.md":           "# bible\n",
			"outline/volumes/v01.md": `# 第一卷

## 本卷目标
- 开局

## 本卷人物
- lin

## 单元索引
| unit_id | 章范围 | 一句话功能 | 终局边界 | 下一钩类型 |
|---|---|---|---|---|
| v01-U1 | 1-3 | a | b | c |

## 卷末状态
- 主角离山
`,
			"continuity/facts.md": `# Continuity facts

### Cursor
- last_committed_ch: 3

### Cast snapshot
| 角色 | 年龄 | 职位 | 位置 | 目标 | 伤势/资源 | 知情范围 | 关系质态 |
|------|------|------|------|------|-----------|----------|----------|
| lin | 16 | 外门 | 山下 | 追查 | 无 | 不知 | 独行 |

## Open loops
| FS-id | 内容 | 状态 | 埋点章 | 计划回收 | 实际回收 |
|-------|------|------|--------|----------|----------|
| FS-01 | 刀柄 | planted | 1 | 8 | |
`,
			"continuity/summaries/v01.md": "## Ch3\n山门一别，钩子留下。\n",
		},
		Units: []domain.UnitInfo{{
			UnitID: "v01-U1", Volume: 1, Index: 1, Title: "开局",
			OutlinePath: "outline/units/v01-U1.yaml",
			OutlineRaw:  "unit_id: v01-U1\nfunction: 开局试炼\nnext_hook:\n  type: 危机\n",
			ProsePath:   "units/v01-U1.md",
			ProseRaw:    "他回头望了一眼山门，没再说话。",
		}},
		Cast: []domain.CastCard{{Stem: "lin", Name: "林尘", Role: "protagonist", Raw: "# 林尘\n"}},
	}
	packet := BuildPlanPacketWithMode(snap, 2, domain.PlanModeVolume, "")
	ids := map[string]bool{}
	for _, b := range packet.Blocks {
		ids[b.ID] = true
		if b.ID == "prev-volume" && strings.Contains(b.Content, strings.Repeat("他回头", 1)) && len(b.Content) > 5000 {
			t.Fatal("prev volume should be excerpt, not prose dump")
		}
	}
	// facts 块已退役：卷纲的"已揭/未揭"改由 continuity 块提供（需要 state.yaml，
	// 本夹具没有，所以不在断言里）。
	for _, id := range []string{"prev-volume", "prev-summary", "prev-unit-bridge", "book-outline"} {
		if !ids[id] {
			t.Fatalf("missing block %s in %+v", id, ids)
		}
	}
	if !strings.Contains(packet.UserPrompt, `"volumeId":"v02"`) {
		t.Fatalf("user payload missing v02: %s", packet.UserPrompt)
	}
	if strings.Contains(findBlockContent(packet, "prev-volume"), "units/v01") {
		t.Fatal("handoff should not dump unit paths as prose")
	}
}

func findBlockContent(p domain.ContextPacket, id string) string {
	for _, b := range p.Blocks {
		if b.ID == id {
			return b.Content
		}
	}
	return ""
}
