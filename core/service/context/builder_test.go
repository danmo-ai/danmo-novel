package contextsvc

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

func loadFixture(t *testing.T) domain.BookSnapshot {
	t.Helper()
	root := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	snap, err := books.LoadBook(root, true)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func findUnit(t *testing.T, snap domain.BookSnapshot, id string) domain.UnitInfo {
	t.Helper()
	for _, u := range snap.Units {
		if u.UnitID == id {
			return u
		}
	}
	t.Fatalf("unit %s not found", id)
	return domain.UnitInfo{}
}

// P0-1: 写作包不再塞整份 facts.md / 整份人物卡 / 完整细纲。
func TestWritePacketFieldLevel(t *testing.T) {
	snap := loadFixture(t)
	unit := findUnit(t, snap, "v01-U1")

	// 给 fixture 注入可检测的整份 facts 与底牌内容。
	snap.Files["continuity/facts.md"] = `# Continuity facts

## Public facts
| Kind | Fact | First seen | Notes |
|------|------|------------|-------|
| shown_fact | 林尘通过试炼 | ch1 | |

## Tracking
### Cursor
- last_committed_ch: 3

### Cast snapshot
| 角色 | 年龄 | 职位 | 位置 | 目标 | 伤势/资源 | 知情范围 | 关系质态 |
|------|------|------|------|------|-----------|----------|----------|
| linchen | 16 | 外门弟子 | 山门 | 入内门 | 无 | 不知账本 | 独来独往 |
| wangmou | 40 | 执事 | 账房 | 掩盖亏空 | 无 | 全知 | 敌对 |

## Open loops
| FS-id | 内容 | 状态 | 埋点章 | 计划回收 | 实际回收 |
|-------|------|------|--------|----------|----------|
| FS-01 | 林尘的刀柄来历 | planted | 1 | 5 | |
| FS-02 | 账房亏空 | planted | 2 | 6 | |
`
	snap.Files["canon/author-lore.md"] = "# Author lore\n\n## 终局真相\n头号宿敌是林尘生父。\n"

	packet := BuildWritePacket(snap, unit, "", 0)

	// 1) 底牌禁读：任何 block 不得引用 author-lore。
	for _, b := range packet.Blocks {
		if IsAuthorOnlySource(b.Source) || IsAuthorOnlySource(b.Label) {
			t.Fatalf("author-lore leaked into write packet: %+v", b)
		}
		if strings.Contains(b.Content, "头号宿敌") {
			t.Fatalf("author secret content leaked: %s", b.ID)
		}
	}
	if err := AssertNoAuthorLore(&packet); err != nil {
		t.Fatal(err)
	}

	// 2) facts 是字段级抽取：有游标，有 on_stage 人物状态，没有无关人物行。
	facts := findBlock(packet, "facts")
	if facts == nil {
		t.Fatal("missing facts block")
	}
	if strings.Contains(facts.Content, "wangmou") {
		t.Fatalf("facts block leaked off-stage character: %s", facts.Content)
	}
	if !strings.Contains(facts.Content, "linchen") {
		t.Fatalf("facts block missing on-stage character: %s", facts.Content)
	}
	// 整份 facts.md 的原始章节标题（如 ## Public facts 表头）不应原样出现。
	if strings.Contains(facts.Content, "## Public facts") {
		t.Fatalf("facts block looks like whole-file dump: %s", facts.Content)
	}

	// 3) 人物卡是压缩快照，不是整份卡。
	cast := findBlock(packet, "cast-linchen")
	if cast == nil {
		t.Fatal("missing cast block")
	}
	if !strings.Contains(cast.Content, "短句") {
		t.Fatalf("cast block missing anchor: %s", cast.Content)
	}
	if !strings.Contains(cast.Content, "POV不知") {
		t.Fatalf("cast block missing POV boundary: %s", cast.Content)
	}
	if len(cast.Content) > 900 {
		t.Fatalf("cast block too large (whole-card?): %d bytes", len(cast.Content))
	}

	// 4) 场面节拍来自 scenes，不是整份 YAML。
	beats := findBlock(packet, "unit-beats")
	if beats == nil {
		t.Fatal("missing beats block")
	}
	if !strings.Contains(beats.Content, "S1") || strings.Contains(beats.Content, "chapter_range") {
		t.Fatalf("beats block not scene-focused: %s", beats.Content)
	}
}

// P0-2: author-lore 进入写作包会触发 fail-fast。
func TestWritePacketRejectsAuthorLore(t *testing.T) {
	p := domain.ContextPacket{
		TaskKind: domain.TaskWrite,
		Blocks:   []domain.ContextBlock{{ID: "x", Label: "作者底牌与终局真相", Source: "canon/author-lore.md"}},
	}
	if err := AssertNoAuthorLore(&p); err == nil {
		t.Fatal("expected禁读 violation")
	}
}

// P1: 人名清洗与角色规范化。
func TestCleanNameAndRole(t *testing.T) {
	if got := CleanName("林尘 (protagonist)"); got != "林尘" {
		t.Fatalf("CleanName=%q", got)
	}
	if got := CleanName("# 林晚（主角）"); got != "林晚" {
		t.Fatalf("CleanName=%q", got)
	}
	if NormalizeRole("hero") != "protagonist" || NormalizeRole("villain") != "volume_antagonist" || NormalizeRole("xyz") != "recurring" {
		t.Fatal("NormalizeRole mapping wrong")
	}
}

func findBlock(p domain.ContextPacket, id string) *domain.ContextBlock {
	for i := range p.Blocks {
		if p.Blocks[i].ID == id {
			return &p.Blocks[i]
		}
	}
	return nil
}

// 追加指令：细纲/正文阶段接受作者指令，且默认的格式与门禁约束必须保留。
// 空指令时 prompt 与旧行为完全一致（防回归）。
func TestStageInstructionAppended(t *testing.T) {
	snap := loadFixture(t)
	unit := findUnit(t, snap, "v01-U1")

	withInstr := BuildWritePacket(snap, unit, "追加要求ABC", 0)
	payload, err := DecodeUserPayload(withInstr.UserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.Instruction, "追加要求ABC") {
		t.Fatalf("write instruction not forwarded: %q", payload.Instruction)
	}
	if !strings.Contains(payload.Instruction, "请按单元合同与场面节拍") {
		t.Fatal("write default format contract must be preserved")
	}

	plain := BuildWritePacket(snap, unit, "", 0)
	plainPayload, err := DecodeUserPayload(plain.UserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plainPayload.Instruction, "补充要求") {
		t.Fatalf("empty instruction must not change prompt: %q", plainPayload.Instruction)
	}

	outline := BuildOutlinePacket(snap, 1, []string{unit.UnitID}, "细纲追加XYZ", 0)
	op, err := DecodeUserPayload(outline.UserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(op.Instruction, "细纲追加XYZ") {
		t.Fatalf("outline instruction not forwarded: %q", op.Instruction)
	}
	if !strings.Contains(op.Instruction, "不得改卷纲已定的 function") {
		t.Fatal("outline gate contract must be preserved")
	}
}
