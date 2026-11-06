package contextsvc

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func previewUnit() domain.UnitInfo {
	return domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 2,
		OutlinePath: "outline/units/v01-U1.yaml",
		OutlineRaw: `unit_id: v01-U1
status: accepted
title_working: "两章"
desire: "甲"
obstacle: "乙"
choice: "丙"
payoff: "丁"
chapter_chars: 2500
on_stage: [lu]
pov: lu
state_deltas: []
chapters:
  - chapter: 1
    goal: "第一件事"
    change: "状态改变一"
    cut_hook: "钩子一"
    day_offset: 0
  - chapter: 2
    goal: "第二件事"
    change: "状态改变二"
    cut_hook: "钩子二"
    day_offset: 1
`,
		ChapterTarget: 2500,
	}
}

// write 预览必须与真实首章请求同形。
//
// 以前预览直接返回 BuildWritePacket：documents 与通用 instruction 都在，但按章
// 变化的四项（chapter / chapterRule / 场面节拍 / previousChapterTail）全缺。
// 作者用预览核对衔接时会得出「上一章尾部根本没带」的结论——实际带上了，只是
// 预览没显示。预览的全部价值在于可信，所以这条必须钉住。
func TestWritePreviewMatchesRealFirstChapter(t *testing.T) {
	unit := previewUnit()
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo", Title: "样本"},
		Units:   []domain.UnitInfo{unit},
	}

	base := BuildWritePacket(snap, unit, "")
	preview := WritePreviewPacket(snap, unit)
	if preview.UserPrompt == base.UserPrompt {
		t.Fatal("write 预览不能与基础包逐字相同：按章字段必须补进去")
	}

	p, err := DecodeUserPayload(preview.UserPrompt)
	if err != nil {
		t.Fatalf("预览 payload 不是合法 JSON: %v", err)
	}
	if _, ok := p.Extra["chapter"]; !ok {
		t.Fatal("预览缺少 chapter")
	}
	if _, ok := p.Extra["chapterRule"]; !ok {
		t.Fatal("预览缺少 chapterRule")
	}
	// 首章没有上一章，不能凭空造一条。
	if _, ok := p.Extra["previousChapterTail"]; ok {
		t.Fatal("首章不应带上一章尾部")
	}
}

// 单元第一章的「上一章」住在上一单元里：必须能从上一单元正文取到。
//
// 这条是最容易漏的衔接：单元边界上的那一章，本单元还没有任何已生成章节，
// 上一单元正文是唯一的来源。漏了它，每个单元的第一章都会像凭空开头。
func TestWriteChapterExtraFallsBackToPreviousUnit(t *testing.T) {
	prev := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 1,
		ProsePath: "units/v01-U1.md",
		ProseRaw:  "## 第1章 起点\n\n上一单元最后一章的正文结尾，应当被下一单元的第一章看到。",
	}
	next := domain.UnitInfo{
		UnitID: "v01-U2", Volume: 1, ChapterFrom: 2, ChapterTo: 2,
		OutlinePath: "outline/units/v01-U2.yaml",
		OutlineRaw: `unit_id: v01-U2
status: accepted
chapters:
  - chapter: 2
    goal: "接着上一章"
    change: "变化"
    cut_hook: "钩子"
    day_offset: 0
`,
	}
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo"},
		Units:   []domain.UnitInfo{prev, next},
	}

	extra := WriteChapterExtra(snap, next, 2, 2500, 2000, 3500, "", nil)
	tail, ok := extra["previousChapterTail"]
	if !ok {
		t.Fatal("单元第一章必须带上上一单元的正文尾部，否则整条衔接就断了")
	}
	body := tail.(map[string]any)["body"].(string)
	if !strings.Contains(body, "上一单元最后一章的正文结尾") {
		t.Fatalf("取到的不是上一单元正文: %q", body)
	}
	if src := tail.(map[string]any)["source"].(string); src != "units/v01-U1.md" {
		t.Fatalf("source 应指向上单元正文，实际 %q", src)
	}
}
