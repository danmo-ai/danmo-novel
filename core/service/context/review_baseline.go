package contextsvc

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/resource/novel"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// ReviewBaseline carries the unit-level 全局基线 every chapter review shares.
// Built once per unit review so each chapter call only re-carries the mutable
// part: that chapter's own prose plus the previous chapter's tail.
type ReviewBaseline struct {
	Contract     string
	TimeContract string
	// Facts 已废弃：连续性状态改由 continuity/state.yaml 提供。
	Facts       string
	CastCards   string // ExtractCastForWriteWithIdentity per on_stage
	Relations   string // extractOnStageRelations
	Foreshadow  string // ForeshadowForReview（本单元负责埋/收的条目）
	Ability     string // ExtractAbilityLimits（能力代价与上限）
	Style       string // ExtractStyle + 写作规则压缩
	LockedTerms string // ExtractLockedTerms 当前卷
	GenreKB     string // GenreBullets
	PrevTail    string // 上单元文末 1200 字（只审第 1 章时用）
	PrevSummary string // latestSummaryTail
	// LocalFindings 是整单元的本地硬门结论。按章审校时必须按章裁剪后再注入：
	// 把整单元塞进每一次单章调用，模型会照着重报别章的问题（章号对不上→触发
	// 一次修复重试），同一个问题也会在候选里出现两遍。
	LocalFindings []domain.ReviewFinding
	Rules         string // 审校清单（本书文件 / 作者库覆盖 / 内置）
	// RulesSource names where Rules came from, so the UI can say "你改的那份正在生效".
	RulesSource string
}

// ReviewRulesSource labels where the checklist text was resolved from.
const (
	RulesFromBook    = "book"    // canon/review-checklist.md（本书专属）
	RulesFromLibrary = "library" // 知识库里的 25-review-checklist（作者编辑过）
	RulesFromBuiltin = "builtin" // 内置文章
)

// ResolveReviewRules picks the checklist text in priority order:
// 本书 canon/review-checklist.md > 作者知识库里的 25-review-checklist > 内置文章。
// 规则本身只是建议（最终判定归模型），所以作者改完应当立刻生效，
// 而不是像以前那样作者库的编辑被内置副本遮蔽。
func ResolveReviewRules(snap domain.BookSnapshot, libraryArticle string) (string, string) {
	if custom := strings.TrimSpace(snap.Files["canon/review-checklist.md"]); custom != "" {
		return custom, RulesFromBook
	}
	if lib := strings.TrimSpace(libraryArticle); lib != "" {
		return lib, RulesFromLibrary
	}
	if builtin := strings.TrimSpace(novel.ReviewChecklistArticle()); builtin != "" {
		return builtin, RulesFromBuiltin
	}
	return "", RulesFromBuiltin
}

// BuildReviewBaseline assembles the immutable unit-level baseline.
// libraryRules is the author's edited 25-review-checklist body (empty = not
// customized); see ResolveReviewRules for precedence.
func BuildReviewBaseline(snap domain.BookSnapshot, unit domain.UnitInfo, localFindings []domain.ReviewFinding, libraryRules string) ReviewBaseline {
	onStage := extractOnStage(unit.OutlineRaw)
	volumeID := fmt.Sprintf("v%02d", unit.Volume)

	b := ReviewBaseline{
		// scenes 已由 BuildChapterReviewPacket 按章注入「场面节拍」块，
		// 这里必须剥掉，否则同一份节拍在审校提示词里出现两遍。
		Contract:     TrimContract(ContractWithoutBeats(unit.OutlineRaw), domain.TaskReview),
		TimeContract: buildTimeGuidance(snap, unit),
		Relations:    extractOnStageRelations(snap.Cast, onStage),
		Foreshadow:   ForeshadowForReview(snap, unit),
		Ability:      ExtractAbilityLimits(snap.Files["canon/world.md"], snap.Files["canon/goldfinger.md"]),
		Style:        ExtractStyle(snap.Files["canon/style-fingerprint.md"]) + "\n\n" + clip(snap.Files["canon/writing-rules.md"], 420),
		LockedTerms:  ExtractLockedTerms(snap.Files["canon/locked-terms.yaml"], volumeID),
		GenreKB:      novel.GenreBullets(snap.Summary.Genre, snap.Summary.Subgenre),
		// 卷摘要可能有几万字，只取尾部；与 BuildWritePacket 的 prev-summary 一致。
		PrevSummary: clipTail(SummaryBeforeUnit(snap, unit), 1200),
	}
	// 规则来源优先级：本书文件 > 作者库覆盖 > 内置文章。
	b.Rules, b.RulesSource = ResolveReviewRules(snap, libraryRules)

	for _, stem := range onStage {
		for _, c := range snap.Cast {
			if c.Stem != stem {
				continue
			}
			identity := identityLabel(snap.IdentitySegments, stem, unit.UnitID)
			card := ExtractCastForWriteWithIdentity(c, extractPOV(unit.OutlineRaw), identity)
			if card != "" {
				b.CastCards += "\n" + card
			}
		}
	}
	b.CastCards = strings.TrimSpace(b.CastCards)

	if prev := previousUnit(snap, unit); prev != nil {
		b.PrevTail = clipTail(prev.ProseRaw, 800)
	}

	b.LocalFindings = append([]domain.ReviewFinding(nil), localFindings...)
	return b
}

// localGateForChapter renders only this chapter's local gate findings.
// chapter == 0 的结论是全单元级的（例如整单元字数），每章都带上。
func localGateForChapter(findings []domain.ReviewFinding, chapter int) string {
	var fb strings.Builder
	for _, f := range findings {
		if f.Chapter != 0 && f.Chapter != chapter {
			continue
		}
		fmt.Fprintf(&fb, "- [%s] %s: %s\n", f.Severity, f.Title, f.Evidence)
	}
	return fb.String()
}

// chapterContextBlocks is the single block list both审校 and 修订 build on.
//
// 两份调用必须产出**逐字节相同**的 blocks，前缀缓存才可能命中：审校与修订看到
// 的是同一章正文、同一份合同、同一批在场人物与约束，本就该共用一份上下文。
// 曾经两边各写一套：修订只有 8 块、审校 12 块，第 5 块顺序还不同——结果就是
// 13819 字的共享正文一个 token 都没命中。
//
// 审校清单只服务 review；修订共享人物、事实、正文和时序，不带整套审稿规则。
func chapterContextBlocks(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, prevTail string, baseline ReviewBaseline, reviewRules bool) []domain.ContextBlock {
	blocks := []domain.ContextBlock{}
	add := func(id, label, source, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
	}

	add("unit-contract", "单元合同", unit.OutlinePath, baseline.Contract)
	add("time-contract", "本章时间合同", unit.OutlinePath, baseline.TimeContract)
	add("continuity", "连续性状态", books.StatePath, ExtractContinuity(snap, unit, extractOnStage(unit.OutlineRaw), ContinuityForReview))
	add("relations", "在场关系", "canon/cast", baseline.Relations)
	add("foreshadows", "本单元伏笔", books.ForeshadowSchedulePath, baseline.Foreshadow)
	add("ability-limits", "能力上限", "canon/goldfinger.md", baseline.Ability)
	add("style", "风格与写作规则", "canon/style-fingerprint.md", baseline.Style)
	add("locked-terms", "锁词", "canon/locked-terms.yaml", baseline.LockedTerms)
	add("genre-kb", "题材写时约束", "knowledge:"+snap.Summary.Genre, baseline.GenreKB)
	// 规则来源写进 Source，面板上作者能看出生效的是哪一份。
	rulesSource := "knowledge:25-review-checklist.md"
	switch baseline.RulesSource {
	case RulesFromBook:
		rulesSource = "canon/review-checklist.md"
	case RulesFromLibrary:
		rulesSource = "knowledge:25-review-checklist.md（作者已编辑）"
	}
	if reviewRules {
		add("review-rules", "审校清单", rulesSource, baseline.Rules)
	}
	add("cast", "在场人物", "canon/cast", baseline.CastCards)
	if prevTail == "" && baseline.PrevSummary != "" {
		// 首章没有"本单元上一章"，带上单元摘要；其余章带上一章末。
		add("prev-summary", "最近章摘要", "continuity/actual", baseline.PrevSummary)
	} else if prevTail != "" {
		add("prev-tail", "上一章末", unit.ProsePath, prevTail)
	}
	// 以下两块是**每章都不同**的，必须排在最后。
	//
	// 前缀缓存按字节比对最长公共前缀：卡在一起的两章只有这两块不一样，只要把
	// 它们压到尾部，前面 9 个共享基线块（审校清单 2600 字、揭示约束 1100 字、
	// 在场人物 800 字…）就能逐字复用。曾经 unit-beats 排第 2，于是相邻两章的
	// 公共前缀只有 19 字节（0.1%），基线每章按全价重算。
	add("unit-beats", "本章场面节拍", unit.OutlinePath, BeatsForChapter(unit.OutlineRaw, ch.Chapter))
	add("prose", "本章正文", unit.ProsePath, ch.Body)
	return blocks
}

// BuildChapterReviewPacket assembles the per-chapter packet.
// Baseline blocks are identical across chapters; the only variable parts are
// the chapter's own prose and the previous chapter's tail (both static).
func BuildChapterReviewPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, prevTail string, baseline ReviewBaseline) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskReview, 1)

	blocks := chapterContextBlocks(snap, unit, ch, prevTail, baseline, true)
	hashes := map[string]string{unit.ProsePath: shortHash(ch.Body)}
	for _, b := range blocks {
		hashes[b.ID+"|"+b.Source] = shortHash(b.Content)
	}

	// system 只放两种任务都成立的规则，契约随 instruction 下发——这样审校与
	// 修订的 system 逐字相同，user payload 的正文段也相同，前缀才可能复用。
	system := novel.ChapterStageSystemPrompt()
	extra := map[string]any{}
	// 只注入本章的本地硬门结论：整单元的塞进来会被模型当成"这一章的问题"重报。
	if fb := localGateForChapter(baseline.LocalFindings, ch.Chapter); fb != "" {
		extra["localFindings"] = fb
	}
	instruction := fmt.Sprintf("只审第 %d 章（%s），按审校清单逐条执行。\n\n%s",
		ch.Chapter, ch.Title, novel.ReviewOutputContract())
	// book 只放整本书恒定的字段。章号必须挪到 extra（排在 documents 之后）：
	// book 是 payload 的第一个字段，章号放进去会让相邻两章的请求从第 19 字节
	// 就分叉，前面所有共享基线块全部失效。
	extra["reviewChapter"] = ch.Chapter
	user := BuildUserPrompt("review", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID,
	}, instruction, blocks, extra)

	packet := domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskReview, BookID: snap.Summary.ID, UnitID: unit.UnitID,
		InputHashes: hashes, Blocks: blocks,
		KnowledgeVersion: novel.KnowledgeVersion, EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2,
		Policy: policy, SystemPrompt: system, UserPrompt: user,
	}
	if err := AssertNoAuthorLore(&packet); err != nil {
		panic(err)
	}
	return packet
}

// revisePacket assembles a revise packet from the shared block set plus one contract.
//
// prevTail 必须与审校那条路径传同一个值：blocks 里「上一章末 / 最近章摘要」
// 的选择依赖它，选错会让同一章的两份包在那一块分叉，前缀缓存白丢。
func revisePacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, prevTail string, baseline ReviewBaseline, system, task, instruction string) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskRevise, 0)
	// 每章最多 3 次：补丁 → 补丁修复 → 整章兜底。三次互相独立（锚点错、
	// 修完仍然套不上、只能在正文层面重写），所以必须是三段而不是两段的加长。
	policy.MaxCalls = 3
	policy.AllowOneRepair = true

	blocks := chapterContextBlocks(snap, unit, ch, prevTail, baseline, false)
	hashes := map[string]string{unit.ProsePath: shortHash(ch.Body)}
	for _, b := range blocks {
		hashes[b.ID+"|"+b.Source] = shortHash(b.Content)
	}

	extra := map[string]any{
		"reviseChapter":  ch.Chapter,
		"reviseFindings": reviseFindingRows(findings),
	}
	// book 只放整本书恒定的字段。章号必须挪到 extra（排在 documents 之后）：
	// book 是 payload 的第一个字段，章号放进去会让相邻两章的请求从第 19 字节
	// 就分叉，前面所有共享基线块全部失效。
	user := BuildUserPrompt(task, "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID,
	}, instruction, blocks, extra)

	packet := domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskRevise, BookID: snap.Summary.ID, UnitID: unit.UnitID,
		InputHashes: hashes, Blocks: blocks,
		KnowledgeVersion: novel.KnowledgeVersion, EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2,
		Policy: policy, SystemPrompt: system, UserPrompt: user,
	}
	if err := AssertNoAuthorLore(&packet); err != nil {
		panic(err)
	}
	return packet
}

// BuildReviseChapterPacket assembles the per-chapter revise patch packet.
// Scope is hard-bounded to one chapter: the caller verifies the returned chapter
// matches. Output is a find/replace patch, not the whole chapter — see
// applyChapterEdits for how it is materialized.
//
// system 与审校逐字相同（ChapterStageSystemPrompt），契约走 instruction：
// 这样勾选后这次修订的 prompt 前缀几乎全部命中审校留下的缓存。
func BuildReviseChapterPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, prevTail string, baseline ReviewBaseline) domain.ContextPacket {
	return revisePacket(snap, unit, ch, findings, prevTail, baseline,
		novel.ChapterStageSystemPrompt(), "revise_patch",
		fmt.Sprintf("只修订第 %d 章，针对 reviseFindings 列出的问题给出最小改动。\n\n%s",
			ch.Chapter, novel.RevisePatchOutputContract(ch.Chapter)))
}

// BuildReviseChapterFallbackPacket is the whole-chapter contract used only after
// the patch path has failed: it is the last attempt before the run errors out.
// Blocks 与 system 都与补丁包相同，只有 instruction 里的契约不同——所以前两次
// 失败不会把这次也变成冷启动。
func BuildReviseChapterFallbackPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, prevTail string, baseline ReviewBaseline) domain.ContextPacket {
	return revisePacket(snap, unit, ch, findings, prevTail, baseline,
		novel.ChapterStageSystemPrompt(), "revise",
		fmt.Sprintf("只修订第 %d 章，针对 reviseFindings 列出的问题最小改动。\n\n%s",
			ch.Chapter, novel.ReviseBodyOutputContract(ch.Chapter)))
}

// BuildFinalizeChapterPacket assembles one chapter's actual-ledger reconcile packet.
//
// 定稿是逐章核对，所以它和审校/修订受同一条缓存约束：documents 里只放**整个单元
// 每章都一样**的基线（单元合同、时间合同、连续性、本单元伏笔、在场人物与关系、
// 锁词），按章变化的只有本章正文，而且必须排在基线之后；章号、本章计划、本章
// 伏笔计划一律走 extra（序列化在 documents/instruction 之后）。
//
// 旧版把「本章计划」放成第一个 document，相邻两章从 prompt 的第 20 个字节就
// 分叉——实测 6 章定稿的命中率只有 6–10%，每章都把同一份上下文按全价重算。
func BuildFinalizeChapterPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, baseline ReviewBaseline) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskFinalize, 1)
	// 每章最多 2 次：核对 → 一次结构/证据修复。
	policy.MaxCalls = 2
	policy.AllowOneRepair = true

	blocks := []domain.ContextBlock{}
	add := func(id, label, source, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
	}
	// 以上共享块必须逐字相同：它们的顺序一旦按章变化，本章正文之后的一切都会失效。
	add("unit-contract", "单元合同", unit.OutlinePath, baseline.Contract)
	add("time-contract", "时间合同", unit.OutlinePath, baseline.TimeContract)
	add("continuity", "连续性状态", books.StatePath, ExtractContinuity(snap, unit, extractOnStage(unit.OutlineRaw), ContinuityForReview))
	add("foreshadows", "本单元伏笔", books.ForeshadowSchedulePath, baseline.Foreshadow)
	add("cast", "在场人物", "canon/cast", baseline.CastCards)
	add("relations", "在场关系", "canon/cast", baseline.Relations)
	add("locked-terms", "锁词", "canon/locked-terms.yaml", baseline.LockedTerms)
	// 以下每章都不同，必须排在共享基线之后。
	add("prose", "本章正文", unit.ProsePath, ch.Body)

	hashes := map[string]string{unit.ProsePath: shortHash(ch.Body)}
	for _, b := range blocks {
		hashes[b.ID+"|"+b.Source] = shortHash(b.Content)
	}
	system := novel.FinalizeReconcileSystemPrompt()
	extra := map[string]any{
		"finalizeChapter":   ch.Chapter,
		"chapterPlan":       ChapterContract(unit.OutlineRaw, ch.Chapter),
		"chapterForeshadow": ForeshadowForChapter(snap, unit, ch.Chapter),
		// 计划伏笔的 ID 清单要单独给出来：模型必须逐条回答"到底发生了没有"，
		// 藏在自然语言里它只会挑提到的那几条，漏掉的会在接受时整份被拒。
		"plannedForeshadow": plannedLoopsForChapter(unit, ch.Chapter),
	}
	// book 只放整本书恒定的字段；章号进 extra，否则 statements 从第一个字段就分叉。
	instruction := fmt.Sprintf("只核对第 %d 章实际正文。不得把计划自动视为已发生。", ch.Chapter)
	user := BuildUserPrompt("finalize_reconcile", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID,
	}, instruction, blocks, extra)

	return domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskFinalize, BookID: snap.Summary.ID, UnitID: unit.UnitID,
		InputHashes: hashes, Blocks: blocks,
		KnowledgeVersion: novel.KnowledgeVersion, EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2,
		Policy: policy, SystemPrompt: system, UserPrompt: user,
	}
}

// BuildFinalizeUnitPacket assembles the whole-unit ledger packet (one call).
//
// 输入只有「基线 + 全单元正文」，篇幅上完全不值得拆成六次：六次调用会把同一
// 份基线重复携带六遍，而产出只是一张账本。但一份 JSON 承载全单元意味着任何
// 一章不合规都整份作废，所以调用方（RunFinalize）必须保留逐章兜底。
func BuildFinalizeUnitPacket(snap domain.BookSnapshot, unit domain.UnitInfo, chapters []domain.ChapterDraft, baseline ReviewBaseline) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskFinalize, 1)
	// 两次：整单元核对 → 一次定向修复（补漏章 / 重抄回证）。
	policy.MaxCalls = 2
	policy.AllowOneRepair = true

	blocks := []domain.ContextBlock{}
	add := func(id, label, source, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
	}
	add("unit-contract", "单元合同", unit.OutlinePath, baseline.Contract)
	add("time-contract", "时间合同", unit.OutlinePath, baseline.TimeContract)
	add("continuity", "连续性状态", books.StatePath, ExtractContinuity(snap, unit, extractOnStage(unit.OutlineRaw), ContinuityForReview))
	add("foreshadows", "本单元伏笔", books.ForeshadowSchedulePath, baseline.Foreshadow)
	add("cast", "在场人物", "canon/cast", baseline.CastCards)
	add("relations", "在场关系", "canon/cast", baseline.Relations)
	add("locked-terms", "锁词", "canon/locked-terms.yaml", baseline.LockedTerms)
	// 全单元正文作为一个块：整份正文就是这一次调用的唯一变动部分。
	add("prose", "本单元正文", unit.ProsePath, unit.ProseRaw)

	hashes := map[string]string{unit.ProsePath: shortHash(unit.ProseRaw)}
	for _, b := range blocks {
		hashes[b.ID+"|"+b.Source] = shortHash(b.Content)
	}
	// 章号清单与逐章计划走 extra：它们是"要求覆盖哪些章"的清单，不是正文。
	chapterList := make([]int, 0, len(chapters))
	plans := make([]map[string]any, 0, len(chapters))
	for _, ch := range chapters {
		chapterList = append(chapterList, ch.Chapter)
		plans = append(plans, map[string]any{
			"chapter":           ch.Chapter,
			"plan":              ChapterContract(unit.OutlineRaw, ch.Chapter),
			"foreshadow":        ForeshadowForChapter(snap, unit, ch.Chapter),
			"plannedForeshadow": plannedLoopsForChapter(unit, ch.Chapter),
		})
	}
	extra := map[string]any{
		"finalizeChapters": chapterList,
		"chapterPlans":     plans,
	}
	instruction := fmt.Sprintf("逐章核对第 %d–%d 章的实际正文，一章一条，不得遗漏。不得把计划自动视为已发生。",
		chapters[0].Chapter, chapters[len(chapters)-1].Chapter)
	user := BuildUserPrompt("finalize_reconcile_unit", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID,
	}, instruction, blocks, extra)
	system := novel.FinalizeUnitReconcileSystemPrompt()

	return domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskFinalize, BookID: snap.Summary.ID, UnitID: unit.UnitID,
		InputHashes: hashes, Blocks: blocks,
		KnowledgeVersion: novel.KnowledgeVersion, EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2,
		Policy: policy, SystemPrompt: system, UserPrompt: user,
	}
}

// plannedLoopsForChapter lists the foreshadow ids this chapter must check.
//
// 接受路径会逐条要求 plannedForeshadowHandled 覆盖它们；把清单直接写进包体，
// 模型才知道"这几条都要回答"，而不是只挑正文里提到的那几条。
func plannedLoopsForChapter(unit domain.UnitInfo, chapter int) []string {
	if chapter <= 0 {
		return []string{}
	}
	plant, payoff := books.ChapterLoops(unit)
	out := make([]string, 0, len(plant[chapter])+len(payoff[chapter]))
	seen := map[string]bool{}
	for _, id := range append(append([]string{}, plant[chapter]...), payoff[chapter]...) {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// reviseFindingRows renders author-selected findings for the revision prompt.
func reviseFindingRows(findings []domain.ReviewFinding) []map[string]any {
	rows := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		row := map[string]any{"id": f.ID, "severity": f.Severity, "title": f.Title, "lens": f.Lens}
		if f.Chapter > 0 {
			row["chapter"] = f.Chapter
		}
		if f.Evidence != "" {
			row["evidence"] = f.Evidence
		}
		if f.Suggestion != "" {
			row["suggestion"] = f.Suggestion
		}
		rows = append(rows, row)
	}
	return rows
}
