package contextsvc

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestWriteRefineCarriesOriginalProse(t *testing.T) {
	snap := loadFixture(t)
	unit := snap.Units[0]
	unit.ProseRaw = "## 第1章 旧标题\n\n旧正文第一句，城门在雨里关上。\n"

	fresh := BuildWritePacketFor(snap, unit, "", domain.RewriteFresh)
	if b := findBlock(fresh, "original-prose"); b != nil {
		t.Fatalf("fresh write must not carry the old draft: %+v", b)
	}
	if strings.Contains(fresh.UserPrompt, "优化底稿") {
		t.Fatal("fresh instruction must not mention the old draft")
	}

	refine := BuildWritePacketFor(snap, unit, "", domain.RewriteRefine)
	block := findBlock(refine, "original-prose")
	if block == nil {
		t.Fatal("refine write must carry the old draft")
	}
	if !strings.Contains(block.Content, "旧正文第一句") {
		t.Fatalf("original-prose block missing draft: %s", block.Content)
	}
	if !strings.Contains(refine.UserPrompt, "优化底稿") {
		t.Fatal("refine instruction must point at the old draft")
	}
}

func TestWriteRefineWithoutProseFallsBackToFresh(t *testing.T) {
	snap := loadFixture(t)
	unit := snap.Units[0]
	unit.ProseRaw = ""

	packet := BuildWritePacketFor(snap, unit, "", domain.RewriteRefine)
	if b := findBlock(packet, "original-prose"); b != nil {
		t.Fatalf("empty draft must not produce a block: %+v", b)
	}
	if strings.Contains(packet.UserPrompt, "优化底稿") {
		t.Fatal("empty draft must not claim an optimization basis")
	}
}

// 同一次 run 里每一章的请求都必须带着同一份底稿：按章变化的内容只能走 extra，
// 否则 documents 从前缀中段就分叉，后面的块与 references 全部按全价重算。
func TestWriteRefineOriginalBlockStableAcrossChapters(t *testing.T) {
	snap := loadFixture(t)
	unit := snap.Units[0]
	unit.ProseRaw = "## 第1章 一\n\n甲。\n\n## 第2章 二\n\n乙。\n"

	packet := BuildWritePacketFor(snap, unit, "", domain.RewriteRefine)
	for _, ch := range []int{1, 2, 3} {
		narrowed, _ := NarrowBeats(packet, unit, ch)
		b := findBlock(narrowed, "original-prose")
		if b == nil || b.Content != strings.TrimSpace(unit.ProseRaw) {
			t.Fatalf("chapter %d lost the stable original block", ch)
		}
	}
}
