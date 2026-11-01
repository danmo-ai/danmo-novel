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
	LocalGate   string // 本单元本地硬门一行文本
	Rules       string // 审校清单（本书文件 / 作者库覆盖 / 内置）
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

	if len(localFindings) > 0 {
		var fb strings.Builder
		for _, f := range localFindings {
			fmt.Fprintf(&fb, "- [%s] %s: %s\n", f.Severity, f.Title, f.Evidence)
		}
		b.LocalGate = fb.String()
	}
	return b
}

// chapterReviewMaxOutputTokens bounds a single-chapter review. One chapter only
// needs 1-3 findings, so the JSON stays tiny; the rest of the budget is headroom
// for the thinking chain, which shares max_tokens with the answer.
const chapterReviewMaxOutputTokens = 12288

// BuildChapterReviewPacket assembles the per-chapter packet.
// Baseline blocks are identical across chapters; the only variable parts are
// the chapter's own prose and the previous chapter's tail (both static).
func BuildChapterReviewPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, prevTail string, baseline ReviewBaseline, modelMaxOut int) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskReview, 1)
	policy.MaxOutputTokens = CapMaxOutput(chapterReviewMaxOutputTokens, modelMaxOut)

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
	if baseline.LocalGate != "" {
		extra["localFindings"] = baseline.LocalGate
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

// chapterReviseMaxOutputTokens bounds a single-chapter revision. The answer is
// one full chapter (2-4k 汉字 ≈ 3-6k tokens) plus the thinking chain, so the
// 8192 default truncates and forces a wasted retry.
const chapterReviseMaxOutputTokens = 16384

// BuildReviseChapterPacket assembles the per-chapter revise packet.
// Scope is hard-bounded to one chapter: the system prompt says so and the
// caller verifies the returned chapter matches.
func BuildReviseChapterPacket(snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, baseline ReviewBaseline, modelMaxOut int) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskRevise, 0)
	policy.MaxCalls = 2
	policy.AllowOneRepair = true
	policy.MaxOutputTokens = CapMaxOutput(chapterReviseMaxOutputTokens, modelMaxOut)

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

	hashes := map[string]string{unit.ProsePath: shortHash(ch.Body)}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
	}

	system := novel.ReviseChapterSystemPrompt(ch.Chapter)
	extra := map[string]any{
		"reviseChapter":  ch.Chapter,
		"reviseFindings": reviseFindingRows(findings),
	}
	user := BuildUserPrompt("revise", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID, "chapter": ch.Chapter,
	}, fmt.Sprintf("只修订第 %d 章，针对列出的问题最小改动，输出单章 JSON。", ch.Chapter), blocks, extra)

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
