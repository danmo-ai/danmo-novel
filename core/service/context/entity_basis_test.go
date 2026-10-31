package contextsvc

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func basisSnap() domain.BookSnapshot {
	return domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo", Genre: "刑侦"},
		Files: map[string]string{
			"outline/book_outline.md":   "# 总纲\n分卷走向 v1-v8\n",
			"book-bible.md":             "# bible\n读者承诺\n",
			"outline/volumes/v02.md":    "# 第二卷\n## 本卷人物\n- lin\n",
			"canon/reveal-schedule.md":  "# reveal\n| 信息 | 最早卷 |\n| 刀柄 | v03 |\n",
			"canon/world.md":            "# world\n",
			"canon/writing-rules.md":    "# rules\n",
			"canon/style-fingerprint.md": "# style\n",
			"continuity/facts.md":       "### Cursor\n- last_committed_ch: 3\n",
			"canon/author-lore.md":      "# secret\n真相\n",
		},
		Volumes: []domain.VolumeInfo{{Volume: 2, Raw: "# 第二卷\n## 本卷人物\n- lin\n", Cast: []string{"lin"}}},
		Cast: []domain.CastCard{
			{Stem: "lin", Name: "林尘", Role: "protagonist", Raw: "# 林尘\n"},
			{Stem: "other", Name: "旁人", Role: "recurring", Raw: "# 旁人\n"},
		},
	}
}

func TestClassifySettingLayers(t *testing.T) {
	if ClassifySetting("canon/reveal-schedule.md") != LayerDerived {
		t.Fatal("reveal should be derived")
	}
	if ClassifySetting("canon/cast/lin.md") != LayerDerived {
		t.Fatal("cast should be derived")
	}
	if ClassifySetting("canon/writing-rules.md") != LayerCraft {
		t.Fatal("rules should be craft")
	}
	if ClassifySetting("continuity/facts.md") != LayerLedger {
		t.Fatal("facts should be ledger")
	}
}

func TestRewriteCastCarriesOutlineBasis(t *testing.T) {
	snap := basisSnap()
	p := BuildRewriteEntityPacket(snap, domain.TaskSpec{
		Kind: domain.TaskRewriteEntity, TargetPath: "canon/cast/lin.md", EntityKind: "cast",
	}, 0)
	ids := map[string]bool{}
	for _, b := range p.Blocks {
		ids[b.ID] = true
		if b.ID == "basis-book-outline" && !strings.Contains(b.Content, "分卷走向") {
			t.Fatalf("outline basis missing: %s", b.Content)
		}
	}
	for _, id := range []string{"current", "basis-book-outline", "basis-volume", "basis-reveal"} {
		if !ids[id] {
			t.Fatalf("missing %s in %v", id, ids)
		}
	}
	if !strings.Contains(p.UserPrompt, "总纲派生") && !strings.Contains(p.UserPrompt, `"layer":"总纲派生"`) {
		t.Fatalf("user payload should mark derived layer: %s", p.UserPrompt)
	}
}

func TestRewriteCraftDoesNotCarryOutline(t *testing.T) {
	snap := basisSnap()
	p := BuildRewriteEntityPacket(snap, domain.TaskSpec{
		Kind: domain.TaskRewriteEntity, TargetPath: "canon/writing-rules.md", EntityKind: "rules",
	}, 0)
	for _, b := range p.Blocks {
		if strings.HasPrefix(b.ID, "basis-book-outline") {
			t.Fatal("craft rewrite should not need outline")
		}
	}
}

func TestWritePacketIncludesRevealConstraint(t *testing.T) {
	snap := basisSnap()
	snap.Units = []domain.UnitInfo{{
		UnitID: "v02-U1", Volume: 2, Index: 1, ChapterFrom: 1, ChapterTo: 1,
		OutlinePath: "outline/units/v02-U1.yaml", ProsePath: "units/v02-U1.md",
		OutlineRaw: "unit_id: v02-U1\non_stage: [lin]\n",
	}}
	unit := snap.Units[0]
	p := BuildWritePacket(snap, unit, "", 0)
	var reveal string
	for _, b := range p.Blocks {
		if b.ID == "reveal" {
			reveal = b.Content
		}
	}
	if reveal == "" {
		t.Fatal("write packet missing reveal block")
	}
	if !strings.Contains(reveal, "v02") && !strings.Contains(reveal, "揭示约束") {
		t.Fatalf("reveal block: %s", reveal)
	}
}

func TestVolumeCastStemsNotWholeBook(t *testing.T) {
	snap := basisSnap()
	// 当前卷只有 lin；other 不应进入卷纲人物上下文。
	got := VolumeCastStems(snap, 2)
	for _, s := range got {
		if s == "other" {
			t.Fatalf("unrelated cast leaked: %v", got)
		}
	}
	found := false
	for _, s := range got {
		if s == "lin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing on-volume protagonist: %v", got)
	}
}

func TestExtractRevealForVolumeSplitsLocked(t *testing.T) {
	raw := `# Reveal
| 信息 | 最早卷 |
|------|--------|
| 刀柄来历 | v03 |
| 开篇规则 | v01 |
- 不得写穿终局真相
`
	got := ExtractRevealForVolume(raw, 2)
	if !strings.Contains(got, "揭示约束") {
		t.Fatalf("missing header: %s", got)
	}
	if strings.Contains(got, "可暗示/可确认：") && strings.Contains(got, "刀柄来历") {
		// v03 should be locked at v02
		if strings.Contains(got, "可暗示/可确认：\n| 刀柄") {
			t.Fatalf("v03 leaked as allowed: %s", got)
		}
	}
}
