package contextsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// 每章只带本章正文 + 上一章末；全局基线共享但不含整份卷摘要。
// 卷摘要可达数万字，整份塞进首章会让那一次调用比别的章贵三倍。
func TestChapterReviewPacketClipsSummaryAndScopesProse(t *testing.T) {
	summary := strings.Repeat("卷摘要行。", 20000) // 远超任何合理上下文预算
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b1", Genre: "悬疑"},
		Files: map[string]string{
			"continuity/summaries/v02.md": summary,
		},
	}
	unit := domain.UnitInfo{
		UnitID: "v02-U1", Volume: 2, ChapterFrom: 1, ChapterTo: 3,
		OutlinePath: "outline/units/v02-U1.yaml",
		ProsePath:   "units/v02-U1.md",
		OutlineRaw:  "unitId: v02-U1\nword_target: 9000\n",
	}
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	if len([]rune(baseline.PrevSummary)) > 1300 {
		t.Fatalf("prev summary not clipped: %d runes", len([]rune(baseline.PrevSummary)))
	}

	ch := domain.ChapterDraft{Chapter: 2, Title: "门", Body: "第2章正文"}
	packet := BuildChapterReviewPacket(snap, unit, ch, "上一章末尾", baseline, 128000)

	var prose, prevTail, contract string
	for _, b := range packet.Blocks {
		switch b.ID {
		case "prose":
			prose = b.Content
		case "prev-tail":
			prevTail = b.Content
		case "unit-contract":
			contract = b.Content
		}
		if b.ID == "prev-summary" {
			t.Fatal("non-first chapter must not carry the unit summary")
		}
	}
	if prose != ch.Body {
		t.Fatalf("prose block=%q want only this chapter", prose)
	}
	if prevTail != "上一章末尾" {
		t.Fatalf("prev tail=%q", prevTail)
	}
	if contract == "" {
		t.Fatal("unit contract missing from baseline")
	}
	// 单章预算必须收窄：整单元路径才用 32768。
	if packet.Policy.MaxOutputTokens >= 32768 {
		t.Fatalf("per-chapter budget=%d should be below the unit-wide 32768", packet.Policy.MaxOutputTokens)
	}
}

// 审校清单来源优先级：本书文件 > 作者库编辑 > 内置文章。
func TestReviewBaselineCarriesChecklistAndResolvesSource(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 1, OutlineRaw: "unitId: v01-U1\n"}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b1"}, Files: map[string]string{}}

	builtin := BuildReviewBaseline(snap, unit, nil, "")
	if !strings.Contains(builtin.Rules, "审校规则") || builtin.RulesSource != RulesFromBuiltin {
		t.Fatalf("built-in checklist missing: source=%q", builtin.RulesSource)
	}

	// 作者在知识库页编辑过 → 编辑内容必须生效（以前被内置副本遮蔽）。
	edited := "# 我的审校规则\n- 只查连续性和 POV"
	fromLib := BuildReviewBaseline(snap, unit, nil, edited)
	if fromLib.Rules != edited || fromLib.RulesSource != RulesFromLibrary {
		t.Fatalf("library edit should win over builtin: source=%q", fromLib.RulesSource)
	}

	// 本书专属文件优先级最高。
	bookRules := "# 本书审校规则\n- 只允许 1 条 blocking"
	snap.Files["canon/review-checklist.md"] = bookRules
	fromBook := BuildReviewBaseline(snap, unit, nil, edited)
	if fromBook.Rules != bookRules || fromBook.RulesSource != RulesFromBook {
		t.Fatalf("book override should win, got source=%q", fromBook.RulesSource)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 注入给模型的清单不能带 YAML front matter：`stages:` / `default:` 是应用元数据，
// 对模型是纯噪音，也会让它以为自己在读配置文件。
func TestReviewRulesBlockHasNoFrontMatter(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 1,
		OutlinePath: "outline/units/v01-U1.yaml", ProsePath: "units/v01-U1.md",
		OutlineRaw: "unitId: v01-U1\n",
	}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b1"}, Files: map[string]string{}}
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	if baseline.RulesSource != RulesFromBuiltin {
		t.Fatalf("source=%q", baseline.RulesSource)
	}
	if strings.HasPrefix(strings.TrimSpace(baseline.Rules), "---") {
		t.Fatalf("front matter leaked into the prompt:\n%s", baseline.Rules[:40])
	}
	for _, bad := range []string{"stages:", "default: true", "audience:"} {
		if strings.Contains(baseline.Rules, bad) {
			t.Errorf("metadata %q leaked into the rules block", bad)
		}
	}
	if !strings.Contains(baseline.Rules, "审校规则") {
		t.Fatal("checklist body missing")
	}
}

// scenes 曾经被注入两遍：整份 YAML 合同里一份，「场面节拍」块又一份。
// 实测占写作提示词 35%（70KB → 45KB），必须保证合同里不再含 scenes。
func TestContractWithoutBeatsRemovesScenes(t *testing.T) {
	raw := "unit_id: v01-U1\ndesire: 站住脚\nscenes:\n  id: S1\n  chapter: 1\n  beat: 入场\nnext_hook:\n  out: 门外有人\n"
	got := ContractWithoutBeats(raw)
	if strings.Contains(got, "scenes:") || strings.Contains(got, "beat: 入场") {
		t.Fatalf("scenes leaked into the contract:\n%s", got)
	}
	for _, want := range []string{"unit_id: v01-U1", "desire: 站住脚", "next_hook:"} {
		if !strings.Contains(got, want) {
			t.Errorf("contract lost %q:\n%s", want, got)
		}
	}
}

// 合同只剩 scenes 时不能返回空串——宁可原样返回，也不能让模型拿到空合同。
func TestContractWithoutBeatsFallsBackWhenOnlyScenes(t *testing.T) {
	raw := "scenes:\n  id: S1\n  beat: 入场\n"
	if got := ContractWithoutBeats(raw); strings.TrimSpace(got) == "" {
		t.Fatal("must not blank out a contract that is only scenes")
	}
}

// 分章写作只该带本章的场面：6 章单元每次带全部 26 个场面是纯浪费。
func TestBeatsForChapterNarrowsToOneChapter(t *testing.T) {
	raw := `scenes:
  id: S1
  chapter: 1
  beat: 入场
  id: S2
  chapter: 1
  beat: 加压
  id: S3
  chapter: 2
  beat: 转折
`
	all := ChapterBeats(raw)
	one := BeatsForChapter(raw, 1)
	two := BeatsForChapter(raw, 2)
	if len(one) >= len(all) || len(two) >= len(all) {
		t.Fatalf("filter did not narrow: all=%d ch1=%d ch2=%d", len(all), len(one), len(two))
	}
	if !strings.Contains(one, "beat: 入场") || !strings.Contains(one, "beat: 加压") {
		t.Fatalf("chapter 1 lost its beats:\n%s", one)
	}
	if strings.Contains(one, "beat: 转折") {
		t.Fatalf("chapter 2's beat leaked into chapter 1:\n%s", one)
	}
	if !strings.Contains(two, "beat: 转折") {
		t.Fatalf("chapter 2 beats missing:\n%s", two)
	}
}

// 没有 chapter 字段的场面一律保留：宁可多带，也不要丢掉作者的节拍。
func TestBeatsForChapterKeepsUnlabelledScenes(t *testing.T) {
	raw := `scenes:
  id: S1
  chapter: 1
  beat: 入场
  id: S2
  beat: 无章号
`
	if got := BeatsForChapter(raw, 2); !strings.Contains(got, "无章号") {
		t.Fatalf("unlabelled scene dropped:\n%s", got)
	}
}

// 过滤后为空说明 chapter 标记不可信，退回全量而不是给模型一个空节拍。
func TestBeatsForChapterFallsBackWhenNothingMatches(t *testing.T) {
	raw := `scenes:
  id: S1
  chapter: 9
  beat: 入场
  id: S2
  chapter: 9
  beat: 加压
`
	got := BeatsForChapter(raw, 1)
	if !strings.Contains(got, "beat: 入场") {
		t.Fatalf("should fall back to the full block, got:\n%s", got)
	}
}

// 审校按章调用，所以每章只该带本章的场面节拍。
// 回归：Contract 剥掉 scenes 后必须在 build 时按章补回来，否则模型盲审。
func TestChapterReviewPacketCarriesOwnBeats(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 2,
		OutlinePath: "outline/units/v01-U1.yaml", ProsePath: "units/v01-U1.md",
		OutlineRaw: `unit_id: v01-U1
desire: 站住脚
scenes:
  id: S1
  chapter: 1
  beat: 第一章的进场
  id: S2
  chapter: 2
  beat: 第二章的转折
`,
	}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b1"}, Files: map[string]string{}}
	baseline := BuildReviewBaseline(snap, unit, nil, "")

	if strings.Contains(baseline.Contract, "scenes:") {
		t.Fatal("contract must not repeat scenes")
	}

	ch1 := BuildChapterReviewPacket(snap, unit, domain.ChapterDraft{Chapter: 1, Body: "正文一"}, "", baseline, 0)
	ch2 := BuildChapterReviewPacket(snap, unit, domain.ChapterDraft{Chapter: 2, Body: "正文二"}, "", baseline, 0)

	var b1, b2 string
	for _, b := range ch1.Blocks {
		if b.ID == "unit-beats" {
			b1 = b.Content
		}
	}
	for _, b := range ch2.Blocks {
		if b.ID == "unit-beats" {
			b2 = b.Content
		}
	}
	if b1 == "" || b2 == "" {
		t.Fatal("unit-beats block missing: the model would review blind")
	}
	if !strings.Contains(b1, "第一章的进场") || strings.Contains(b1, "第二章的转折") {
		t.Fatalf("chapter 1 beats wrong:\n%s", b1)
	}
	if !strings.Contains(b2, "第二章的转折") || strings.Contains(b2, "第一章的进场") {
		t.Fatalf("chapter 2 beats wrong:\n%s", b2)
	}
}

// 连续性状态按阶段裁剪：写作不读知情范围（会诱导提前揭示），审校才读。
func TestContinuityStageScoping(t *testing.T) {
	root := t.TempDir() + "/book"
	src := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	if err := copyTree(src, root); err != nil {
		t.Fatal(err)
	}
	state := `version: 1
units:
  - unit: v01-U0
    volume: 1
    cast:
      linchen:
        location: 山门
        goal: 入内门
        injury: 左臂扭伤
        knows: [知道刀柄来历]
`
	if err := os.WriteFile(filepath.Join(root, books.StatePath), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	unit := snap.Units[0]
	onStage := []string{"linchen"}

	write := ExtractContinuity(snap, unit, onStage, ContinuityForWrite)
	if !strings.Contains(write, "山门") || !strings.Contains(write, "入内门") {
		t.Fatalf("write stage missing state: %s", write)
	}
	if strings.Contains(write, "知情范围") || strings.Contains(write, "刀柄来历") {
		t.Fatalf("write stage must not expose knowledge scope: %s", write)
	}

	review := ExtractContinuity(snap, unit, onStage, ContinuityForReview)
	if !strings.Contains(review, "知情范围") {
		t.Fatalf("review stage must expose knowledge scope: %s", review)
	}
}

// 没有状态账时返回空串而不是「尚无事实」这类占位：那是噪音。
func TestContinuityEmptyWithoutLedger(t *testing.T) {
	snap := loadFixtureCopy(t)
	unit := findUnit(t, snap, "v01-U1")
	if got := ExtractContinuity(snap, unit, nil, ContinuityForWrite); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// 包体积回归：写作单章 ≤ 32KB、审校单章 ≤ 34KB。
//
// 上限用 fixture 校准过（fixture 单元小），主要作用是挡住"把整份 YAML 又塞回去"
// 这类回归——scenes 曾重复注入两次、facts.md 曾把整份账本拖进来，都靠这条线拦住。
func TestPacketSizeBudget(t *testing.T) {
	snap := loadFixtureCopy(t)
	unit := findUnit(t, snap, "v01-U1")

	wp := BuildWritePacket(snap, unit, "", 0)
	narrowed := NarrowBeats(wp, unit, unit.ChapterFrom)
	if sum := blockBytes(narrowed.Blocks); sum > 32*1024 {
		t.Fatalf("write packet %d B exceeds 32KB budget", sum)
	}

	baseline := BuildReviewBaseline(snap, unit, nil, "")
	ch := domain.ChapterDraft{Chapter: unit.ChapterFrom, Body: "正文"}
	rp := BuildChapterReviewPacket(snap, unit, ch, "", baseline, 0)
	if sum := blockBytes(rp.Blocks); sum > 34*1024 {
		t.Fatalf("review packet %d B exceeds 34KB budget", sum)
	}

	// scenes 不得同时出现在合同与节拍块里。
	for _, b := range narrowed.Blocks {
		if b.ID == "unit-contract" && strings.Contains(b.Content, "scenes:") {
			t.Fatal("contract must not repeat scenes")
		}
	}
}

func blockBytes(blocks []domain.ContextBlock) int {
	sum := 0
	for _, b := range blocks {
		sum += b.Bytes
	}
	return sum
}

// 审校合同裁掉规划字段，写作合同裁掉作者台账；两者都不该带 continuity_risks。
func TestTrimContractPerStage(t *testing.T) {
	raw := `unit_id: v01-U1
function: 入门
word_target: "9000"
pleasure: 翻盘
forbidden: 不揭底
info_control: 只谈案子
continuity_risks: 作者登记
observations: 历轮观察
scenes:
  - id: S1
`
	write := TrimContract(raw, domain.TaskWrite)
	for _, gone := range []string{"continuity_risks", "observations", "word_target", "function"} {
		if strings.Contains(write, gone) {
			t.Errorf("write contract kept %q", gone)
		}
	}
	for _, kept := range []string{"forbidden", "info_control", "pleasure"} {
		if !strings.Contains(write, kept) {
			t.Errorf("write contract dropped %q", kept)
		}
	}

	review := TrimContract(raw, domain.TaskReview)
	for _, gone := range []string{"continuity_risks", "word_target", "function", "pleasure"} {
		if strings.Contains(review, gone) {
			t.Errorf("review contract kept %q", gone)
		}
	}
	if !strings.Contains(review, "forbidden") {
		t.Error("review contract must keep forbidden (it judges release boundaries)")
	}
}
