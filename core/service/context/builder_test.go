package contextsvc

import (
	"fmt"
	"os"
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

// loadFixtureCopy copies the fixture book into a temp dir so a test can write
// continuity/state.yaml without touching the repository fixtures.
func loadFixtureCopy(t *testing.T) domain.BookSnapshot {
	t.Helper()
	src := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	dst := filepath.Join(t.TempDir(), "book")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(dst, false)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
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
	snap := loadFixtureCopy(t)
	unit := findUnit(t, snap, "v01-U1")

	// 连续性状态来自 continuity/state.yaml：只含 on_stage 人物与未回收伏笔。
	state := `version: 1
units:
  - unit: v01-U0
    volume: 1
    after_ch: 0
    cast:
      linchen:
        location: 山门
        goal: 入内门
        injury: 无
        knows: [不知账本]
      wangmou:
        location: 账房
        goal: 掩盖亏空
    loops:
      - {id: FS-01, status: planted, detail: 林尘的刀柄来历}
`
	if err := os.MkdirAll(filepath.Join(snap.Summary.RootPath, "continuity"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snap.Summary.RootPath, books.StatePath), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
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

	// 2) 连续性状态来自 continuity/state.yaml：只含 on_stage 人物，不含无关人物。
	continuity := findBlock(packet, "continuity")
	if continuity == nil {
		t.Fatal("missing continuity block")
	}
	if strings.Contains(continuity.Content, "wangmou") {
		t.Fatalf("continuity block leaked off-stage character: %s", continuity.Content)
	}
	if !strings.Contains(continuity.Content, "linchen") {
		t.Fatalf("continuity block missing on-stage character: %s", continuity.Content)
	}
	// 写作不给知情范围：那会诱导模型提前揭示 POV 不知道的事。
	if strings.Contains(continuity.Content, "知情范围") {
		t.Fatalf("write packet must not expose knowledge scope: %s", continuity.Content)
	}
	// 不能是整份账本的转储。
	if strings.Contains(continuity.Content, "Continuous facts") || strings.Contains(continuity.Content, "Cast snapshot") {
		t.Fatalf("continuity block looks like whole-file dump: %s", continuity.Content)
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

	// 4) 章合同来自 chapters，不是整份 YAML，也不依赖 scene ID。
	beats := findBlock(packet, "unit-beats")
	if beats == nil {
		t.Fatal("missing chapter-plan block")
	}
	for _, want := range []string{"goal:", "change:", "cut_hook:", "day_offset:"} {
		if !strings.Contains(beats.Content, want) {
			t.Fatalf("chapter plan missing %s: %s", want, beats.Content)
		}
	}
	if strings.Contains(beats.Content, "chapter_range") {
		t.Fatalf("chapter plan leaked full contract: %s", beats.Content)
	}
}

func TestWritePacketCarriesTheChapterLengthTarget(t *testing.T) {
	// 写的时候必须知道这一章该多长，否则只能猜，写完才被硬门拦。
	unit := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 2,
		OutlinePath: "outline/units/v01-U1.yaml",
		OutlineRaw:  "unit_id: v01-U1\nword_target: 10000\n",
	}
	packet := BuildWritePacket(domain.BookSnapshot{Summary: domain.BookSummary{ID: "b"}}, unit, "", 0)
	var text string
	for _, b := range packet.Blocks {
		if b.ID == "chapter-length" {
			text = b.Content
		}
	}
	if text == "" {
		t.Fatal("write packet is missing the chapter-length block")
	}
	// 10000 / 2 章 = 5000 → 夹到上限 3500；提示词里的数字必须与硬门一致。
	target := books.ChapterTarget(unit)
	if target != domain.ChapterCharsMax {
		t.Fatalf("chapterTarget=%d want %d", target, domain.ChapterCharsMax)
	}
	floor, ceil := domain.ChapterBand(target)
	for _, want := range []string{
		"目标 3500 字",
		fmt.Sprintf("%d–%d 字", floor, ceil),
	} {

		if !strings.Contains(text, want) {
			t.Fatalf("length guidance missing %q:\n%s", want, text)
		}
	}
}

func TestChapterContractNarrowsToCurrentChapterWithoutScenes(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 2, OutlinePath: "outline/units/v01-U1.yaml", OutlineRaw: "unit_id: v01-U1\ndesire: 立足\nobstacle: 质疑\nchoice: 复查\npayoff: 获得许可\nforbidden: 禁揭\non_stage: [lin]\npov: lin\nstory_day: 3\nend_day: 4\nchapters:\n  - chapter: 1\n    goal: 第一章目标\n    change: 第一章变化\n    cut_hook: 第一章钩子\n    day_offset: 0\n  - chapter: 2\n    goal: 第二章目标\n    change: 第二章变化\n    cut_hook: 第二章钩子\n    day_offset: 1\n"}
	packet := BuildWritePacket(domain.BookSnapshot{Summary: domain.BookSummary{ID: "b"}}, unit, "", 0)
	narrowed, beats := NarrowBeats(packet, unit, 2)
	if !strings.Contains(beats, "第二章目标") || strings.Contains(beats, "第一章目标") {
		t.Fatalf("chapter-specific beats were not narrowed: %s", beats)
	}
	// 节拍必须离开 documents：留在里面会让后面所有块与 references 失去前缀复用。
	if strings.Contains(narrowed.UserPrompt, "第二章目标") || strings.Contains(narrowed.UserPrompt, "第一章目标") {
		t.Fatalf("beats must not stay in documents: %s", narrowed.UserPrompt)
	}
	before, err := DecodeUserPayload(packet.UserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	after, err := DecodeUserPayload(narrowed.UserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]UserDoc{}
	for _, d := range before.Documents {
		if d.ID == "unit-beats" {
			continue
		}
		want[d.ID] = d
	}
	if len(after.Documents) != len(want) {
		t.Fatalf("documents=%d want %d (只有 unit-beats 该被摘掉)", len(after.Documents), len(want))
	}
	for _, d := range after.Documents {
		if d.ID == "unit-beats" {
			t.Fatal("unit-beats block must be removed, not rewritten")
		}
		if want[d.ID] != d {
			t.Fatalf("chapter narrowing changed unrelated document: %+v", d)
		}
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

// 写作包不带输出上限：0 = 交给 LLM client 按模型目录的 max_output 决定。
// 上层写死 8192 时，声明 384000 的模型被按 8k 用，思考 tokens 吃满后正文从
// 中间被切断，报出来的却是"没有 JSON"。
func TestWritePacketSendsNoOutputCap(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 6,
		OutlinePath: "outline/units/v01-U1.yaml", OutlineRaw: "chapter_chars: 2500\n",
	}
	packet := BuildWritePacket(domain.BookSnapshot{Summary: domain.BookSummary{ID: "b"}}, unit, "", 384000)
	if packet.Policy.MaxOutputTokens != 0 {
		t.Fatalf("maxOutputTokens=%d want 0 (model config decides)", packet.Policy.MaxOutputTokens)
	}
	// 按章调用仍由章节数量决定，与输出上限无关。
	if !packet.Policy.SplitByChapter {
		t.Fatal("a 6-chapter unit must still be written one chapter per call")
	}
}

// 字数约束必须出现在模型真正会读的三处：系统提示词、额度块、单章指令。
// 之前只写在额度块里，而且措辞是「多于 3500 字会有超长告警」——实测一本
// 目标 2500 字/章的书六章写出 3160–6210 字。
func TestLengthContractReachesEveryPromptSurface(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 6,
		OutlinePath: "outline/units/v01-U1.yaml", OutlineRaw: "chapter_chars: 2500\n",
	}
	packet := BuildWritePacket(domain.BookSnapshot{Summary: domain.BookSummary{ID: "b"}}, unit, "", 384000)

	// 1) 系统提示词：硬规则里带确切目标，且写明不是参考值。
	if !strings.Contains(packet.SystemPrompt, "每章 2500 字") {
		t.Fatalf("system prompt lost the per-chapter target:\n%s", packet.SystemPrompt)
	}
	if !strings.Contains(packet.SystemPrompt, "合同值不是参考值") {
		t.Fatalf("system prompt must say the target is a contract:\n%s", packet.SystemPrompt)
	}
	// 2) 额度块：只讲单章，给出区间与后果。
	var guidance string
	for _, b := range packet.Blocks {
		if b.ID == "chapter-length" {
			guidance = b.Content
		}
	}
	floor, ceil := domain.ChapterBand(2500)
	for _, want := range []string{"目标 2500 字", fmt.Sprintf("%d–%d", floor, ceil), "会被退回重写", "判为不合格"} {
		if !strings.Contains(guidance, want) {
			t.Fatalf("length guidance missing %q:\n%s", want, guidance)
		}
	}
	// 场面数不写死：细纲没给节拍时给区间，每场字数也有区间。
	for _, want := range []string{"2–4 个主线场面", fmt.Sprintf("%d–%d 字", domain.SceneCharsMin, domain.SceneCharsMax)} {
		if !strings.Contains(guidance, want) {
			t.Fatalf("length guidance missing scene range %q:\n%s", want, guidance)
		}
	}
	if strings.Contains(guidance, "3 个场面") || strings.Contains(guidance, "约 3 个") {
		t.Fatalf("length guidance must not hardcode three scenes:\n%s", guidance)
	}
	// 单元章数不固定，不折算单元总量：只说这一章。
	for _, banned := range []string{"本单元", "单元总", "共 6 章"} {
		if strings.Contains(guidance, banned) {
			t.Fatalf("length guidance must not talk about the unit total (%q):\n%s", banned, guidance)
		}
	}
	// 3) 超长只是 advisory：审校页没法对整章做大改，不能把它做成阻断面。
	//    这条由 gates 的测试守（这里不引 gates 包，避免循环依赖语义混乱）。
}
