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
			"outline/book_outline.md":      "# 总纲\n分卷走向 v1-v8\n",
			"book-bible.md":                "# bible\n读者承诺\n",
			"outline/volumes/v02.md":       "# 第二卷\n## 本卷人物\n- lin\n",
			"canon/foreshadow-schedule.md": "# 伏笔登记表\n- schema_version: 1\n",
			"canon/world.md":               "# world\n",
			"canon/writing-rules.md":       "# rules\n",
			"canon/style-fingerprint.md":   "# style\n",
			"continuity/facts.md":          "### Cursor\n- last_committed_ch: 3\n",
			"canon/author-lore.md":         "# secret\n真相\n",
		},
		Volumes: []domain.VolumeInfo{{Volume: 2, Raw: "# 第二卷\n## 本卷人物\n- lin\n", Cast: []string{"lin"}}},
		Cast: []domain.CastCard{
			{Stem: "lin", Name: "林尘", Role: "protagonist", Raw: "# 林尘\n"},
			{Stem: "other", Name: "旁人", Role: "recurring", Raw: "# 旁人\n"},
		},
	}
}

func TestClassifySettingLayers(t *testing.T) {
	if ClassifySetting("canon/foreshadow-schedule.md") != LayerDerived {
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
	})
	ids := map[string]bool{}
	for _, b := range p.Blocks {
		ids[b.ID] = true
		if b.ID == "basis-book-outline" && !strings.Contains(b.Content, "分卷走向") {
			t.Fatalf("outline basis missing: %s", b.Content)
		}
	}
	for _, id := range []string{"current", "basis-book-outline", "basis-volume", "basis-foreshadow"} {
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
	})
	for _, b := range p.Blocks {
		if strings.HasPrefix(b.ID, "basis-book-outline") {
			t.Fatal("craft rewrite should not need outline")
		}
	}
}

func TestWritePacketIncludesAbilityLimits(t *testing.T) {
	// 设定规则此前从不进入写作包：world/goldfinger 只在立项注入，写作层
	// 只有锁词与信息边界，能力上限完全缺席。它现在必须一直在场。
	snap := basisSnap()
	snap.Files["canon/world.md"] = "# world\n\n## 力量 / 规则\n- 谁能做什么：共享感官\n- 上限：不能读心\n"
	snap.Files["canon/goldfinger.md"] = "| 资源 / 能力 | 谁能使用 | 代价 | 上限 | 例外与限制 | 最近使用 |\n|---|---|---|---|---|---|\n| 泰斗之眼 | 陆沉观察 | 受她感官限制 | 不能隔空得到信息 | - | - |\n"
	snap.Units = []domain.UnitInfo{{
		UnitID: "v02-U1", Volume: 2, Index: 1, ChapterFrom: 1, ChapterTo: 1,
		OutlinePath: "outline/units/v02-U1.yaml", ProsePath: "units/v02-U1.md",
		OutlineRaw: "unit_id: v02-U1\non_stage: [lin]\n",
	}}
	p := BuildWritePacket(snap, snap.Units[0], "")
	var ability string
	for _, b := range p.Blocks {
		if b.ID == "ability-limits" {
			ability = b.Content
		}
	}
	if ability == "" {
		t.Fatal("write packet missing ability-limits block")
	}
	for _, want := range []string{"泰斗之眼", "不能隔空得到信息", "不能读心"} {
		if !strings.Contains(ability, want) {
			t.Fatalf("ability block missing %q:\n%s", want, ability)
		}
	}
	// 势力地图这类规划信息不该进写作包。
	if strings.Contains(ability, "势力地图") {
		t.Fatalf("ability block must not carry the faction map:\n%s", ability)
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
