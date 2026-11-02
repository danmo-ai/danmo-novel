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
	Contract string // NormalizeUnitContract(ContractWithoutBeats)
	// Facts 已废弃：连续性状态改由 continuity/state.yaml 提供。
	Facts       string
	CastCards   string // ExtractCastForWriteWithIdentity per on_stage
	Relations   string // extractOnStageRelations
	Reveal      string // ExtractRevealForVolume
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
		Contract:    TrimContract(NormalizeUnitContract(ContractWithoutBeats(unit.OutlineRaw)), domain.TaskReview),
		Relations:   extractOnStageRelations(snap.Cast, onStage),
		Reveal:      ExtractRevealForVolume(snap.Files["canon/reveal-schedule.md"], unit.Volume),
		Style:       ExtractStyle(snap.Files["canon/style-fingerprint.md"]) + "\n\n" + clip(snap.Files["canon/writing-rules.md"], 420),
		LockedTerms: ExtractLockedTerms(snap.Files["canon/locked-terms.yaml"], volumeID),
		GenreKB:     novel.GenreBullets(snap.Summary.Genre, snap.Summary.Subgenre),
		// 卷摘要可能有几万字，只取尾部；与 BuildWritePacket 的 prev-summary 一致。
		PrevSummary: clipTail(latestSummaryTail(snap, unit.Volume), 1200),
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

// BuildChapterReviewPacket assembles the per-chapter packet.
// Baseline blocks are identical across chapters; the only variable parts are
// the chapter's own prose and the previous chapter's tail (both static).
func BuildChapterReviewPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, prevTail string, baseline ReviewBaseline, modelMaxOut int) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskReview, 1)

	blocks := []domain.ContextBlock{}
	add := func(id, label, source, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
	}

	add("unit-contract", "单元合同", unit.OutlinePath, baseline.Contract)
	// 只带本章的场面节拍。审校按章调用，整单元 20+ 个场面逐章重复注入
	// 是最大的单项浪费；Contract 里的 scenes 已被 ContractWithoutBeats 剥掉，
	// 这里是唯一的场面来源，缺了它就等于让模型盲审。
	add("unit-beats", "本章场面节拍", unit.OutlinePath, BeatsForChapter(unit.OutlineRaw, ch.Chapter))
	add("continuity", "连续性状态", books.StatePath, ExtractContinuity(snap, unit, extractOnStage(unit.OutlineRaw), ContinuityForReview))
	add("relations", "在场关系", "canon/cast", baseline.Relations)
	add("reveal", "揭示约束", "canon/reveal-schedule.md", baseline.Reveal)
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
	add("review-rules", "审校清单", rulesSource, baseline.Rules)
	add("cast", "在场人物", "canon/cast", baseline.CastCards)
	if baseline.PrevSummary != "" && prevTail == baseline.PrevTail {
		// 首章没有"本单元上一章"，带上单元摘要；其余章带上一章末。
		add("prev-summary", "最近章摘要", "continuity/summaries", baseline.PrevSummary)
	} else if prevTail != "" {
		add("prev-tail", "上一章末", unit.ProsePath, prevTail)
	}
	add("prose", "本章正文", unit.ProsePath, ch.Body)

	hashes := map[string]string{unit.ProsePath: shortHash(ch.Body)}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
	}

	system := novel.ReviewSystemPrompt()
	extra := map[string]any{}
	// 只注入本章的本地硬门结论：整单元的塞进来会被模型当成"这一章的问题"重报。
	if fb := localGateForChapter(baseline.LocalFindings, ch.Chapter); fb != "" {
		extra["localFindings"] = fb
	}
	user := BuildUserPrompt("review", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID, "chapter": ch.Chapter,
	}, fmt.Sprintf("只审第 %d 章（%s），按审校清单逐条执行，输出 JSON。", ch.Chapter, ch.Title), blocks, extra)

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

// reviseChapterBlocks is the block set both revise packets share.
//
// 补丁包与兜底包必须看到完全一样的东西：只差输出契约。否则"补丁失败后兜底"
// 会因为少了某块约束而写出与前一次不同的结果，作者面对两个互斥的正文无从判断。
func reviseChapterBlocks(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, baseline ReviewBaseline) []domain.ContextBlock {
	blocks := []domain.ContextBlock{}
	add := func(id, label, source, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
	}

	add("unit-contract", "单元合同", unit.OutlinePath, baseline.Contract)
	// 修订同样按章进行：只带本章节拍，避免整单元 scenes 重复。
	add("unit-beats", "本章场面节拍", unit.OutlinePath, BeatsForChapter(unit.OutlineRaw, ch.Chapter))
	add("continuity", "连续性状态", books.StatePath, ExtractContinuity(snap, unit, extractOnStage(unit.OutlineRaw), ContinuityForReview))
	add("relations", "在场关系", "canon/cast", baseline.Relations)
	add("cast", "在场人物", "canon/cast", baseline.CastCards)
	add("reveal", "揭示约束", "canon/reveal-schedule.md", baseline.Reveal)
	add("style", "风格与写作规则", "canon/style-fingerprint.md", baseline.Style)
	add("prose", "本章正文", unit.ProsePath, ch.Body)
	return blocks
}

// revisePacket assembles a revise packet from a shared block set plus one contract.
func revisePacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, blocks []domain.ContextBlock, system, task, instruction string) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskRevise, 0)
	// 每章最多 3 次：补丁 → 补丁修复 → 整章兜底。三次互相独立（锚点错、
	// 修完仍然套不上、只能在正文层面重写），所以必须是三段而不是两段的加长。
	policy.MaxCalls = 3
	policy.AllowOneRepair = true

	hashes := map[string]string{unit.ProsePath: shortHash(ch.Body)}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
	}

	extra := map[string]any{
		"reviseChapter":  ch.Chapter,
		"reviseFindings": reviseFindingRows(findings),
	}
	user := BuildUserPrompt(task, "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID, "chapter": ch.Chapter,
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
// Scope is hard-bounded to one chapter: the system prompt says so and the
// caller verifies the returned chapter matches. Output is a find/replace patch,
// not the whole chapter — see applyChapterEdits for how it is materialized.
func BuildReviseChapterPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, baseline ReviewBaseline, modelMaxOut int) domain.ContextPacket {
	blocks := reviseChapterBlocks(snap, unit, ch, baseline)
	return revisePacket(snap, unit, ch, findings, blocks,
		novel.ReviseChapterPatchPrompt(ch.Chapter), "revise_patch",
		fmt.Sprintf("只修订第 %d 章：针对列出的问题给出最小改动的补丁，输出单章补丁 JSON。", ch.Chapter))
}

// BuildReviseChapterFallbackPacket is the whole-chapter contract used only after
// the patch path has failed: it is the last attempt before the run errors out.
func BuildReviseChapterFallbackPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, baseline ReviewBaseline, modelMaxOut int) domain.ContextPacket {
	blocks := reviseChapterBlocks(snap, unit, ch, baseline)
	return revisePacket(snap, unit, ch, findings, blocks,
		novel.ReviseChapterSystemPrompt(ch.Chapter), "revise",
		fmt.Sprintf("只修订第 %d 章，针对列出的问题最小改动，输出单章正文 JSON。", ch.Chapter))
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
