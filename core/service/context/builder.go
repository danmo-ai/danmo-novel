package contextsvc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/resource/novel"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

const ContractVersion = "novel-v1"

// Plan init/reinit emits book outline + settings.files + cast JSON in one shot.
// 32768 fits a mid-size book (≈2–3 万汉字级) with JSON overhead; CapMaxOutput
// clamps to the assigned model's catalog max_output when known.
const planInitMaxOutputTokens = 32768

// CapMaxOutput returns desired budget, never above modelMax when modelMax > 0.
func CapMaxOutput(desired, modelMax int) int {
	if desired <= 0 {
		desired = 8192
	}
	if modelMax > 0 && desired > modelMax {
		return modelMax
	}
	return desired
}

func DefaultPolicy(kind domain.TaskKind, chapterCount int) domain.CallPolicy {
	p := domain.CallPolicy{
		MaxCalls:        2, // generate + optional one structure repair
		MaxOutputTokens: 8192,
		TimeoutSec:      600,
		AllowOneRepair:  true,
	}
	switch kind {
	case domain.TaskWrite:
		if chapterCount > 0 && chapterCount*3500 > 6000 {
			p.SplitByChapter = true
			// 每章最坏 3 次调用：生成 + 一次结构修复 + 一次硬门修复。
			// 三种失败互相独立（结构不合规 → 修；写成了但字数/英文泄漏 → 也修），
			// 所以上限必须按 3 倍算，否则 UI 上会出现 5/4 这种自相矛盾的预算。
			p.MaxCalls = chapterCount * 3
			p.AllowOneRepair = true
		}
	case domain.TaskReview:
		// 审校输出是结构化 JSON，但思考型模型（deepseek/kimi/glm 等）的 reasoning
		// token 与答案共用 max_tokens：正文越长、强度越高，思维链吃得越多。答案本身
		// 已由 ReviewSystemPrompt 的硬上限（findings<=8、每个字段<=40字）压到 O(1)，
		// 这里留出的余量全部给思维链；仍不够时 runJSON 会在重试时翻倍预算。
		p.MaxCalls = 2
		p.AllowOneRepair = true
		p.MaxOutputTokens = 32768
	case domain.TaskOutline:
		// 重写长单元时输出与既有细纲头同量级（数万字 YAML 的 JSON 镜像），
		// 8192 会在 JSON 中途截断；与立项同级给 32768，仍受模型 max_output clamp。
		p.MaxOutputTokens = 32768
	case domain.TaskRevise, domain.TaskFinalize:
		p.MaxCalls = 1
		p.AllowOneRepair = false
	case domain.TaskLocalEdit, domain.TaskContinue, domain.TaskExpand, domain.TaskPolish, domain.TaskRewriteSel:
		p.MaxCalls = 1
		p.AllowOneRepair = false
		p.MaxOutputTokens = 4096
	}
	return p
}

// withUserInstruction appends the author's 追加指令 to the stage's default
// instruction. The default text carries the output-format and gate contract, so
// it is never replaced — the author's wording is layered on top as 补充要求.
// 手工点击与连续重写走同一个 builder，因此两者此字段完全一致。
func withUserInstruction(base, instruction string) string {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return base
	}
	return base + "\n\n补充要求（作者追加；不得违反上面的输出格式与门禁约束）：\n" + instruction
}

func BuildWritePacket(snap domain.BookSnapshot, unit domain.UnitInfo, instruction string, modelMaxOut int) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskWrite, unit.ChapterTo-unit.ChapterFrom+1)
	policy.MaxOutputTokens = CapMaxOutput(policy.MaxOutputTokens, modelMaxOut)
	if modelMaxOut > 0 && modelMaxOut < 4000 && !policy.SplitByChapter && unit.ChapterTo > unit.ChapterFrom {
		policy.SplitByChapter = true
		policy.MaxCalls = (unit.ChapterTo - unit.ChapterFrom + 1) * 3
		policy.AllowOneRepair = true
	}

	blocks := []domain.ContextBlock{}
	add := func(id, label, source, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{
			ID: id, Label: label, Source: source, Content: content, Bytes: len(content),
		})
	}
	volumeID := fmt.Sprintf("v%02d", unit.Volume)
	onStage := extractOnStage(unit.OutlineRaw)
	pov := extractPOV(unit.OutlineRaw)

	// 1. 当前单元合同：结构化 scenes/beats 优先，时钟字段显式化。
	// scenes/chapters 由下面的「场面节拍」专块提供，这里必须剥掉，否则同一份
	// 节拍会被注入两遍（实测可占写作提示词 17%）。
	add("unit-contract", "单元合同", unit.OutlinePath, TrimContract(NormalizeUnitContract(ContractWithoutBeats(unit.OutlineRaw)), domain.TaskWrite))
	add("unit-beats", "场面节拍", unit.OutlinePath, ChapterBeats(unit.OutlineRaw))

	// 2. on_stage 人物：本单元身份 + 三锚点 + 一句台词 + POV「不知」。
	for _, stem := range onStage {
		for _, c := range snap.Cast {
			if c.Stem == stem {
				identity := identityLabel(snap.IdentitySegments, stem, unit.UnitID)
				add("cast-"+stem, "人物·"+CleanName(c.Name), "canon/cast/"+stem+".md", ExtractCastForWriteWithIdentity(c, pov, identity))
			}
		}
	}

	// 3. 在场关系（仅 on_stage 之间的正式关系表行）。
	add("relations", "在场关系", "canon/cast", extractOnStageRelations(snap.Cast, onStage))

	// 4. 连续性状态：人物当前状态 + 未回收伏笔，来自 continuity/state.yaml。
	// 写作不给知情范围（会诱导提前揭示），审校才给。
	add("continuity", "连续性状态", books.StatePath, ExtractContinuity(snap, unit, onStage, ContinuityForWrite))

	// 5. 上一章 / 上一单元钩子与文末。
	// 卷摘要尾只在"本单元第一章"注入：其余章由上一章正文承担衔接，
	// 两者同给等于把上一段的结尾读两遍。
	// 800 字足够接住文末钩子与语感；再多就是让模型重读上一段结尾。
	if prev := previousUnit(snap, unit); prev != nil {
		if tail := clipTail(prev.ProseRaw, 800); tail != "" {
			add("prev-tail", "上单元文末钩子", prev.ProsePath, tail)
		}
	}
	if unit.ChapterFrom <= 1 || previousUnit(snap, unit) == nil {
		if sum := latestSummaryTail(snap, unit.Volume); sum != "" {
			add("prev-summary", "最近章摘要", "continuity/summaries", clipTail(sum, 1200))
		}
	}

	// 6. 风格与写作规则合成一块：两者都是"怎么写"，分开注入等于让模型读两遍同一类约束。
	add("style-rules", "风格与写作规则", "canon/style-fingerprint.md", mergeStyleRules(snap))

	// 7. 当前卷锁词（不展开未来秘密）。
	add("locked-terms", "锁词", "canon/locked-terms.yaml", ExtractLockedTerms(snap.Files["canon/locked-terms.yaml"], volumeID))
	// 7b. 揭示计划按卷截取：可暗示/可确认 + 禁止写穿（不含底牌细节）。
	add("reveal", "揭示约束", "canon/reveal-schedule.md", ExtractRevealForVolume(snap.Files["canon/reveal-schedule.md"], unit.Volume))

	genreKB := novel.GenreBullets(snap.Summary.Genre, snap.Summary.Subgenre)
	add("genre-kb", "题材写时约束", "knowledge:"+snap.Summary.Genre, genreKB)

	// 时间书写规范：把 story_day/gap 转成正文可执行的时间锚点指令。
	add("time-guidance", "时间书写规范", "knowledge:time", buildTimeGuidance(snap, unit))

	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
	}

	system := novel.WriteSystemPrompt()
	user := BuildUserPrompt("write", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID, "volumeId": volumeID,
	}, withUserInstruction("请按单元合同与场面节拍一次写出全部章节正文。章与章之间不要输出 ---；body 内是纯散文。", instruction), blocks, nil)

	packet := domain.ContextPacket{
		ContractVersion:  ContractVersion,
		TaskKind:         domain.TaskWrite,
		BookID:           snap.Summary.ID,
		UnitID:           unit.UnitID,
		VolumeID:         volumeID,
		InputHashes:      hashes,
		Blocks:           blocks,
		KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens:  (len([]rune(system)) + len([]rune(user))) / 2,
		Policy:           policy,
		SystemPrompt:     system,
		UserPrompt:       user,
	}
	// 生产安全：写作包绝不携带作者底牌。
	if err := AssertNoAuthorLore(&packet); err != nil {
		panic(err) // build-time bug, not runtime data
	}
	return packet
}

// mergeStyleRules joins the style fingerprint and the writing rules into one
// block. Both answer "how should this be written", so two blocks only make the
// model read the same class of constraint twice.
func mergeStyleRules(snap domain.BookSnapshot) string {
	style := ExtractStyle(snap.Files["canon/style-fingerprint.md"])
	rules := clip(snap.Files["canon/writing-rules.md"], 420)
	switch {
	case style == "" && rules == "":
		return ""
	case rules == "":
		return style
	case style == "":
		return "### 写作规则\n" + rules
	}
	return style + "\n\n### 写作规则\n" + rules
}

var reviewContractDrop = []string{
	"source_seed_id",
	"word_target",   // 字数配额由本地硬门判定，审校数不出来
	"pleasure",      // 爽点设计意图，判正文时用不上
	"function",      // 卷纲层的功能定位
	"title_working", // 工作标题
	"chapter_range", // 章号由正文标题给出
	"state_deltas",  // 写作护栏；审校要判的是成果，不是意图
	"gap_from_prev", // 时间锚已进「本单元故事时钟」
	"status",
	"chapters",         // 与场面节拍重复
	"continuity_risks", // 作者登记的风险台账
	"observations",     // 历轮执行观察
	"endgame_boundary", // 卷末终局边界
	"time_label",
}

// writeContractDrop are author-side planning fields. The writer needs the
// contract's promise (desire/obstacle/choice/payoff), the hard limits
// (forbidden + locked terms), the clock, and state_deltas — nothing else.
// continuity_risks / observations / endgame_boundary were never written for the
// model, and on a long unit they are the bulk of the contract.
var writeContractDrop = []string{
	"status",
	"source_seed_id",
	"title_working",
	"function",
	"chapter_range",
	"word_target",
	"chapters",         // 与场面节拍重复
	"continuity_risks", // 作者登记的风险台账
	"observations",     // 历轮执行观察
	"endgame_boundary", // 卷末终局边界，写作时由 reveal 约束表达
	"time_label",
}

// TrimContract drops fields a stage cannot use.
//
// 审校读合同是为了核对"有没有兑现承诺、有没有跑偏"；写作读合同是为了知道
// "这一单元要兑现什么、哪些碰不得"。两者无关字段不同，所以按阶段各有一张
// 裁剪表，而不是一份全量 YAML 塞给两边。
func TrimContract(raw string, stage domain.TaskKind) string {
	var drop []string
	switch stage {
	case domain.TaskReview:
		drop = reviewContractDrop
	case domain.TaskWrite:
		drop = writeContractDrop
	default:
		return raw
	}
	return dropTopLevelKeys(raw, drop)
}

// dropTopLevelKeys removes the given top-level keys and their indented bodies.
func dropTopLevelKeys(raw string, keys []string) string {
	drop := make(map[string]bool, len(keys))
	for _, k := range keys {
		drop[k] = true
	}
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		topLevel := line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")
		if topLevel {
			key := line
			if i := strings.IndexByte(key, ':'); i > 0 {
				key = key[:i]
			}
			skipping = drop[strings.TrimSpace(key)]
			if skipping {
				continue
			}
		}
		if skipping {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func BuildOutlinePacket(snap domain.BookSnapshot, volume int, unitIDs []string, instruction string, modelMaxOut int) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskOutline, 0)
	policy.MaxOutputTokens = CapMaxOutput(policy.MaxOutputTokens, modelMaxOut)
	volTag := fmt.Sprintf("v%02d", volume)
	var volRaw string
	var cast []string
	for _, v := range snap.Volumes {
		if v.Volume == volume {
			volRaw = v.Raw
			cast = v.Cast
			break
		}
	}
	blocks := []domain.ContextBlock{
		{ID: "volume", Label: "卷纲", Source: "outline/volumes/" + volTag + ".md", Content: volRaw, Bytes: len(volRaw)},
	}
	if doc, ok := books.StructuredOutlineFor(snap, "outline/volumes/"+volTag+".md"); ok && doc.Valid() {
		if seedRaw, err := json.Marshal(doc.EntitiesOf("单元")); err == nil {
			blocks = append(blocks, domain.ContextBlock{ID: "unit-seeds", Label: "不可改写的单元种子", Source: "outline/volumes/" + volTag + ".md#units", Content: string(seedRaw), Bytes: len(seedRaw)})
		}
	}
	for _, id := range unitIDs {
		for _, u := range snap.Units {
			if u.UnitID == id {
				blocks = append(blocks, domain.ContextBlock{
					ID: "head-" + id, Label: "细纲头·" + id, Source: u.OutlinePath, Content: u.OutlineRaw, Bytes: len(u.OutlineRaw),
				})
			}
		}
	}
	for _, stem := range cast {
		for _, c := range snap.Cast {
			if c.Stem == stem {
				blocks = append(blocks, domain.ContextBlock{
					ID: "cast-" + stem, Label: "人物·" + CleanName(c.Name), Source: "canon/cast/" + stem + ".md", Content: clip(c.Raw, 1800), Bytes: len(clip(c.Raw, 1800)),
				})
			}
		}
	}
	// 细纲规划需要的连续性：重写已有单元时要知道人物当前处境，避免与已定稿前后矛盾。
	if cont := ExtractContinuityForUnit(snap, unitIDs, cast, ContinuityForReview); cont != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "continuity", Label: "连续性状态", Source: books.StatePath, Content: cont, Bytes: len(cont)})
	}
	if reveal := clip(snap.Files["canon/reveal-schedule.md"], 900); strings.TrimSpace(reveal) != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "reveal", Label: "揭示计划", Source: "canon/reveal-schedule.md", Content: reveal, Bytes: len(reveal)})
	}
	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
	}
	system := novel.OutlineSystemPrompt()
	instr := withUserInstruction("为 unitIds 列出的单元生成/重写细纲合同，输出 {\"units\":[...]}（即使只有 1 个单元也必须用数组）。不得改卷纲已定的 function / next_hook.type；scenes / nextHook / storyDay / gapFromPrev / infoControl 必须给出。已有细纲头时在其基础上补全或优化，不要返回空 units。", instruction)
	user := BuildUserPrompt("outline", "", map[string]any{
		"id": snap.Summary.ID, "volumeId": volTag, "unitIds": unitIDs,
	}, instr, blocks, nil)
	packet := domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskOutline, BookID: snap.Summary.ID, VolumeID: volTag,
		InputHashes: hashes, Blocks: blocks, KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2, Policy: policy,
		SystemPrompt: system, UserPrompt: user,
	}
	if err := AssertNoAuthorLore(&packet); err != nil {
		panic(err)
	}
	return packet
}

func BuildPlanPacket(snap domain.BookSnapshot, volume int) domain.ContextPacket {
	return BuildPlanPacketWithMode(snap, volume, domain.PlanModeVolume, "", 0)
}

// BuildPlanPacketWithMode builds the context for 立项 / 重新立项 / 重写卷纲.
// modelMaxOut is the assigned model's catalog max_output (0 = unknown).
func BuildPlanPacketWithMode(snap domain.BookSnapshot, volume int, mode, instruction string, modelMaxOut int) domain.ContextPacket {
	if mode == "" {
		if strings.TrimSpace(snap.Files["outline/book_outline.md"]) == "" {
			mode = domain.PlanModeInit
		} else {
			mode = domain.PlanModeVolume
		}
	}
	policy := DefaultPolicy(domain.TaskPlan, 0)
	full := mode == domain.PlanModeInit || mode == domain.PlanModeReinit
	if full {
		policy.MaxOutputTokens = CapMaxOutput(planInitMaxOutputTokens, modelMaxOut)
	} else {
		policy.MaxOutputTokens = CapMaxOutput(policy.MaxOutputTokens, modelMaxOut)
	}

	blocks := []domain.ContextBlock{}
	hashes := map[string]string{}
	addFile := func(id, label, source string) {
		content := strings.TrimSpace(snap.Files[source])
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
		hashes[source] = shortHash(content)
	}
	addFile("bible", "作者意图 bible", "book-bible.md")
	if full {
		addFile("world", "世界观", "canon/world.md")
		addFile("goldfinger", "金手指", "canon/goldfinger.md")
		addFile("platform", "平台定位", "canon/platform-positioning.md")
		addFile("rules", "写作规则", "canon/writing-rules.md")
		addFile("style", "风格指纹", "canon/style-fingerprint.md")
		addFile("reveal", "揭示计划", "canon/reveal-schedule.md")
		addFile("author-lore", "作者底牌", "canon/author-lore.md")
		addFile("locked", "锁词", "canon/locked-terms.yaml")
		addFile("book-outline", "全书总纲", "outline/book_outline.md")
		for _, c := range snap.Cast {
			src := "canon/cast/" + c.Stem + ".md"
			blocks = append(blocks, domain.ContextBlock{ID: "cast-" + c.Stem, Label: "人物·" + CleanName(c.Name), Source: src, Content: clip(c.Raw, 1800), Bytes: len(clip(c.Raw, 1800))})
			hashes[src] = shortHash(c.Raw)
		}
	}
	volID := ""
	bookMeta := map[string]any{"id": snap.Summary.ID, "genre": snap.Summary.Genre}
	if !full {
		if volume < 1 {
			volume = 1
		}
		volID = fmt.Sprintf("v%02d", volume)
		bookMeta["volumeId"] = volID
		addFile("book-outline", "全书总纲", "outline/book_outline.md")
		if doc, ok := books.StructuredOutlineFor(snap, "outline/book_outline.md"); ok && doc.Valid() {
			if arc, found := doc.Entity("分卷", volID); found {
				if arcRaw, err := json.Marshal(arc); err == nil {
					blocks = append(blocks, domain.ContextBlock{ID: "parent-volume-arc", Label: "不可改写的总纲分卷合同", Source: "outline/book_outline.md#" + volID, Content: string(arcRaw), Bytes: len(arcRaw)})
					hashes["outline/book_outline.md#"+volID] = shortHash(string(arcRaw))
				}
			}
		}
		if reveal := clip(snap.Files["canon/reveal-schedule.md"], 900); strings.TrimSpace(reveal) != "" {
			blocks = append(blocks, domain.ContextBlock{ID: "reveal", Label: "揭示计划", Source: "canon/reveal-schedule.md", Content: reveal, Bytes: len(reveal)})
			hashes["canon/reveal-schedule.md"] = shortHash(reveal)
		}
		if raw := strings.TrimSpace(snap.Files["outline/volumes/"+volID+".md"]); raw != "" {
			blocks = append(blocks, domain.ContextBlock{ID: "volume", Label: "当前卷纲", Source: "outline/volumes/" + volID + ".md", Content: raw, Bytes: len(raw)})
			hashes["outline/volumes/"+volID+".md"] = shortHash(raw)
		}
		// Cross-volume bridge: excerpt previous volume + ledger, never full prose.
		// Only when targeting volume 2+; rewriting v01 has no previous volume.
		if volume >= 2 {
			prevID := fmt.Sprintf("v%02d", volume-1)
			prevPath := "outline/volumes/" + prevID + ".md"
			prevRaw := strings.TrimSpace(snap.Files[prevPath])
			if prevRaw == "" {
				for _, v := range snap.Volumes {
					if v.Volume == volume-1 {
						prevRaw = v.Raw
						break
					}
				}
			}
			if handoff := ExtractVolumeHandoff(prevRaw); handoff != "" {
				blocks = append(blocks, domain.ContextBlock{ID: "prev-volume", Label: "上一卷截取（承接）", Source: prevPath, Content: handoff, Bytes: len(handoff)})
				hashes[prevPath] = shortHash(handoff)
			}
			if sum := latestSummaryTail(snap, volume-1); strings.TrimSpace(sum) != "" {
				tail := clipTail(sum, 1500)
				src := fmt.Sprintf("continuity/summaries/%s.md", prevID)
				blocks = append(blocks, domain.ContextBlock{ID: "prev-summary", Label: "上一卷章摘要尾", Source: src, Content: tail, Bytes: len(tail)})
				hashes[src] = shortHash(tail)
			}
			if last := lastUnitOfVolume(snap, volume-1); last != nil {
				bridge := extractUnitBridge(*last)
				if bridge != "" {
					blocks = append(blocks, domain.ContextBlock{ID: "prev-unit-bridge", Label: "上卷末单元承接", Source: last.OutlinePath, Content: bridge, Bytes: len(bridge)})
					hashes[last.OutlinePath] = shortHash(bridge)
				}
			}
		}
		// 卷纲需要的"已揭/未揭"来自状态账的最新一段，不再读以前的散文账本。
		if cont := ExtractContinuityLatest(snap, nil, ContinuityForReview); cont != "" {
			blocks = append(blocks, domain.ContextBlock{ID: "continuity", Label: "连续性状态（截至最近定稿）", Source: books.StatePath, Content: cont, Bytes: len(cont)})
			hashes[books.StatePath] = shortHash(cont)
		}
		// 只给本卷在场 + 主角 + 上一卷在场人物，不灌全书卡。
		relevant := VolumeCastStems(snap, volume)
		for _, stem := range relevant {
			for _, c := range snap.Cast {
				if c.Stem != stem {
					continue
				}
				src := "canon/cast/" + c.Stem + ".md"
				body := clip(c.Raw, 1200)
				blocks = append(blocks, domain.ContextBlock{ID: "cast-" + c.Stem, Label: "人物·" + CleanName(c.Name), Source: src, Content: body, Bytes: len(body)})
				hashes[src] = shortHash(c.Raw)
			}
		}
	}

	system := novel.PlanSystemPromptForMode(mode)
	inst := strings.TrimSpace(instruction)
	if inst == "" {
		switch mode {
		case domain.PlanModeInit:
			inst = "首次立项：生成全书总纲、全部设定与人物。不要写卷纲。"
		case domain.PlanModeReinit:
			inst = "重新立项：在保留故事骨架前提下，重写总纲、全部设定与人物。不要写卷纲，不触碰已有单元/正文。"
		default:
			if volume >= 2 {
				inst = fmt.Sprintf("生成/重写第 %d 卷卷纲：承接上一卷终局与公开账本截取，延续未回收伏笔；不要复述正文，不要改总纲/设定/人物卡。", volume)
			} else {
				inst = "只生成/重写当前卷纲与单元索引；不要改总纲、设定或人物卡。"
			}
		}
	}
	user := BuildUserPrompt("plan", mode, bookMeta, inst, blocks, nil)
	return domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskPlan, BookID: snap.Summary.ID,
		VolumeID: volID, InputHashes: hashes, Blocks: blocks, KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2, Policy: policy,
		SystemPrompt: system, UserPrompt: user,
	}
}

// BuildFinalizePacket describes the local finalize check (no LLM call).
func BuildFinalizePacket(snap domain.BookSnapshot, unit domain.UnitInfo) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskFinalize, 0)
	policy.MaxCalls = 0
	policy.AllowOneRepair = false
	blocks := []domain.ContextBlock{
		{ID: "outline", Label: "单元合同", Source: unit.OutlinePath, Content: unit.OutlineRaw, Bytes: len(unit.OutlineRaw)},
		{ID: "prose", Label: "待定稿正文", Source: unit.ProsePath, Content: unit.ProseRaw, Bytes: len(unit.ProseRaw)},
	}
	note := "定稿为本地硬门检查，不调用大模型。确认后生成定稿候选；接受候选才会回写 continuity 账本并将单元标为 reviewed。"
	blocks = append(blocks, domain.ContextBlock{ID: "finalize-note", Label: "定稿说明", Source: "local://finalize", Content: note, Bytes: len(note)})
	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
	}
	system := "本地定稿检查：解析正文章节、跑硬门（字数/空章等）。不发送 LLM。"
	user := BuildUserPrompt("finalize", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID, "status": string(unit.Status),
	}, "对当前单元正文做本地定稿检查。", blocks, nil)
	return domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskFinalize, BookID: snap.Summary.ID, UnitID: unit.UnitID,
		InputHashes: hashes, Blocks: blocks, KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens: 0, Policy: policy, SystemPrompt: system, UserPrompt: user,
	}
}

func BuildReviewPacket(snap domain.BookSnapshot, unit domain.UnitInfo, localFindings []domain.ReviewFinding, modelMaxOut int, libraryRules string) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskReview, 0)
	policy.MaxOutputTokens = CapMaxOutput(policy.MaxOutputTokens, modelMaxOut)
	prose := unit.ProseRaw
	onStage := extractOnStage(unit.OutlineRaw)
	blocks := []domain.ContextBlock{
		{ID: "outline", Label: "单元合同", Source: unit.OutlinePath, Content: unit.OutlineRaw, Bytes: len(unit.OutlineRaw)},
		{ID: "prose", Label: "正文", Source: unit.ProsePath, Content: prose, Bytes: len(prose)},
	}
	// 审校读取连续性状态（含知情范围，用来判断 POV 越权）；绝不读作者底牌。
	if cont := ExtractContinuity(snap, unit, onStage, ContinuityForReview); cont != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "continuity", Label: "连续性状态", Source: books.StatePath, Content: cont, Bytes: len(cont)})
	}
	for _, stem := range onStage {
		for _, c := range snap.Cast {
			if c.Stem == stem {
				identity := identityLabel(snap.IdentitySegments, stem, unit.UnitID)
				if card := ExtractCastForWriteWithIdentity(c, extractPOV(unit.OutlineRaw), identity); card != "" {
					blocks = append(blocks, domain.ContextBlock{ID: "cast-" + stem, Label: "人物·" + CleanName(c.Name), Source: "canon/cast/" + stem + ".md", Content: card, Bytes: len(card)})
				}
			}
		}
	}
	// 审校同样校验信息释放边界（按卷截取，不含底牌）。
	if reveal := ExtractRevealForVolume(snap.Files["canon/reveal-schedule.md"], unit.Volume); reveal != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "reveal", Label: "揭示约束", Source: "canon/reveal-schedule.md", Content: reveal, Bytes: len(reveal)})
	}
	// 整单元路径同样带审校清单，与分章路径来源一致。
	if rules, source := ResolveReviewRules(snap, libraryRules); rules != "" {
		src := "knowledge:25-review-checklist.md"
		switch source {
		case RulesFromBook:
			src = "canon/review-checklist.md"
		case RulesFromLibrary:
			src = "knowledge:25-review-checklist.md（作者已编辑）"
		}
		blocks = append(blocks, domain.ContextBlock{ID: "review-rules", Label: "审校清单", Source: src, Content: rules, Bytes: len(rules)})
	}
	var fb strings.Builder
	for _, f := range localFindings {
		fmt.Fprintf(&fb, "- [%s] %s: %s\n", f.Severity, f.Title, f.Evidence)
	}
	hashes := map[string]string{unit.ProsePath: shortHash(prose)}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
	}
	system := novel.ReviewSystemPrompt()
	extra := map[string]any{}
	if fb.Len() > 0 {
		extra["localFindings"] = fb.String()
	}
	user := BuildUserPrompt("review", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID,
	}, "对正文做一次审校。", blocks, extra)
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

// BuildRewriteEntityPacket builds a single-entity rewrite packet.
// 总纲派生设定（人物/揭示/世界/底牌等）必须携带总纲与相关结构作依据；
// 工艺类只带规则与风格。Accept 只写目标文件，依据只读。
func BuildRewriteEntityPacket(snap domain.BookSnapshot, spec domain.TaskSpec, modelMaxOut int) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskRevise, 0)
	policy.MaxCalls = 2
	policy.AllowOneRepair = true
	policy.MaxOutputTokens = CapMaxOutput(policy.MaxOutputTokens, modelMaxOut)
	target := spec.TargetPath
	raw := snap.Files[target]
	blocks := []domain.ContextBlock{
		{ID: "current", Label: "当前内容", Source: target, Content: raw, Bytes: len(raw)},
	}
	add := func(id, label, source, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
	}
	// 依据：按实体类型组包，避免裸改总纲派生设定。
	AddRewriteBasis(snap, target, add)
	// 工艺约束：所有重写都保留写作规则与风格（截取）。
	if target != "canon/writing-rules.md" {
		if w := clip(snap.Files["canon/writing-rules.md"], 800); w != "" {
			add("rules", "写作规则", "canon/writing-rules.md", w)
		}
	}
	if target != "canon/style-fingerprint.md" {
		if s := ExtractStyle(snap.Files["canon/style-fingerprint.md"]); s != "" {
			add("style", "风格指纹", "canon/style-fingerprint.md", s)
		}
	}
	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
	}
	system := rewriteEntitySystemPrompt(spec.EntityKind)
	instruction := spec.Instruction
	if instruction == "" {
		if IsDerivedSetting(target) {
			instruction = "依据总纲与附带结构对齐本实体；保留既有事实与已公开伏笔，不提前揭示未到卷信息；字段规范化、表达精简。"
		} else {
			instruction = "在不改变既有事实与情节的前提下，优化这个实体：字段规范化、结构补齐、表达精简。"
		}
	}
	user := BuildUserPrompt("rewrite_entity", "", map[string]any{
		"id": snap.Summary.ID, "targetPath": target, "entityKind": spec.EntityKind,
		"layer": string(ClassifySetting(target)),
	}, instruction, blocks, nil)
	packet := domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskRewriteEntity, BookID: snap.Summary.ID,
		InputHashes: hashes, Blocks: blocks, KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2, Policy: policy,
		SystemPrompt: system, UserPrompt: user,
	}
	// Optimizing author-lore itself is allowed; other rewrite packets must not leak it.
	if !IsAuthorOnlySource(target) {
		if err := AssertNoAuthorLore(&packet); err != nil {
			panic(err)
		}
	}
	return packet
}

func rewriteEntitySystemPrompt(kind string) string {
	base := "你是小说设定编辑。重写一个设定实体文件，返回规范化完整内容，不要工具调用。\n通用规范：保留既有事实不虚构；结构清晰；不泄露作者底牌到公开文件。"
	switch kind {
	case "cast":
		return base + "\n人物卡规范：第一行 \"# \"+纯姓名（不含括号说明）；`tagline` 一句话身份说明单独字段；`role` 仅 protagonist/volume_antagonist/recurring；保留三锚点、知识边界、台词、关系表。"
	case "bible":
		return base + "\nBible 规范：读者承诺、logline、主角目标、故事引擎、终局方向（只到卷号，不写真相细节）、风格卡齐全。"
	case "volume":
		return base + "\n卷纲规范：卷目标、本卷人物 stem 列表、单元索引表（unit_id|章范围|一句话功能|终局边界|下一钩类型）、终局边界齐全；不重写单元合同。"
	case "identity":
		return base + "\n身份时间线规范：只修改 canon/identity-timeline.yaml；每个身份段 stem/role_title/org/from_unit 必填，from_unit/to_unit 是已有单元边界；同一人同一单元只能一个身份；不删除已有正文历史，不改正文或人物卡；新身份在规划 Accept 后生效。"
	case "state":
		return base + "\n只规范化 YAML 字段与注释，不改状态值。"
	default:
		return base + "\n保持文件原有章节结构，字段补齐、表达精简。"
	}
}

func BuildLocalEditPacket(snap domain.BookSnapshot, sel domain.TextSelection, instruction string) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskLocalEdit, 0)
	system := novel.LocalEditSystemPrompt()
	blocks := []domain.ContextBlock{
		{ID: "before", Label: "前文", Source: sel.Path, Content: sel.Before, Bytes: len(sel.Before)},
		{ID: "selection", Label: "选区", Source: sel.Path, Content: sel.Text, Bytes: len(sel.Text)},
		{ID: "after", Label: "后文", Source: sel.Path, Content: sel.After, Bytes: len(sel.After)},
	}
	user := BuildUserPrompt("local_edit", "", map[string]any{
		"id": snap.Summary.ID, "path": sel.Path,
	}, instruction, blocks, nil)
	return domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskLocalEdit, BookID: snap.Summary.ID,
		InputHashes: map[string]string{sel.Path: shortHash(sel.Text)}, Blocks: blocks, KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2, Policy: policy,
		SystemPrompt: system, UserPrompt: user,
	}
}

func extractOnStage(yaml string) []string {
	// crude: on_stage: [a, b]
	for _, line := range strings.Split(yaml, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "on_stage:") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "on_stage:"))
			rest = strings.Trim(rest, "[]")
			var out []string
			for _, p := range strings.Split(rest, ",") {
				p = strings.Trim(strings.TrimSpace(p), `"'`)
				if p != "" {
					out = append(out, p)
				}
			}
			return out
		}
	}
	return nil
}

// buildTimeGuidance converts the unit's clock fields into executable prose
// rules: 正文只写相对时间锚点（第 N 天/次日/当晚），不写绝对日期；
// 承接上一单元间隔；闪回必须有时标。
func buildTimeGuidance(snap domain.BookSnapshot, unit domain.UnitInfo) string {
	raw := unit.OutlineRaw
	day := yamlScalarLocal(raw, "story_day")
	endDay := yamlScalarLocal(raw, "end_day")
	gap := yamlScalarLocal(raw, "gap_from_prev")
	if gapDays := yamlScalarLocal(raw, "gap_days"); gapDays != "" {
		gap = "至少 " + gapDays + " 天"
	}
	flashback := yamlScalarLocal(raw, "flashback") == "true"
	var b strings.Builder
	b.WriteString("时间书写规则：\n")
	if day != "" && day != "null" {
		fmt.Fprintf(&b, "- 本单元开场＝故事第 %s 天。正文时间用相对锚点（第 %s 天／次日／当晚／三天后），不得写具体年月日或星期。\n", day, day)
	} else {
		b.WriteString("- 本单元故事日未定：先用相对锚点（次日／几天后／当晚），不要虚构具体日期。\n")
	}
	if endDay != "" {
		fmt.Fprintf(&b, "- 本单元结束不晚于故事第 %s 天；章级 day_offset 从本单元开场日起算，不得越出开闭区间。\n", endDay)
	}
	if gap != "" {
		fmt.Fprintf(&b, "- 与上一单元的时间间隔：%s。开场必须体现这个间隔（如「三天后」「隔天清晨」），不得让角色状态/位置瞬间无解释跳变。\n", gap)
	}
	if flashback {
		b.WriteString("- 本单元含闪回：闪回段落必须有明确时标（如「十年前，那个雨夜」），回到现在时要有切换标记，避免读者分不清时间层。\n")
	} else {
		b.WriteString("- 时间顺序推进，不插无标记闪回。\n")
	}
	b.WriteString("- 昼夜/天气/季节与上一单元保持连续；同一章内时间不得倒流。")
	return b.String()
}

func yamlScalarLocal(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, key+":") {
			v := strings.TrimSpace(strings.TrimPrefix(s, key+":"))
			v = strings.Split(v, " #")[0]
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// identityLabel returns "职务身份 · 机构" for stem at unitID from the timeline.
func identityLabel(segments []domain.IdentitySegment, stem, unitID string) string {
	key := unitOrderKey(unitID)
	var best *domain.IdentitySegment
	for i := range segments {
		s := &segments[i]
		if s.Stem != stem {
			continue
		}
		from := unitOrderKey(s.FromUnit)
		if from > key {
			continue
		}
		if s.ToUnit != "" && unitOrderKey(s.ToUnit) < key {
			continue
		}
		if best == nil || from >= unitOrderKey(best.FromUnit) {
			best = s
		}
	}
	if best == nil {
		return ""
	}
	if best.Org != "" {
		return best.RoleTitle + " · " + best.Org
	}
	return best.RoleTitle
}

func unitOrderKey(unitID string) int {
	var v, u int
	fmt.Sscanf(unitID, "v%d-U%d", &v, &u)
	return v*1000 + u
}

// extractPOV reads the pov: scalar from unit YAML.
func extractPOV(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pov:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "pov:")), `"'`)
		}
	}
	return ""
}

// extractOnStageRelations returns relation edges between on-stage stems.
// Prefers formal ## 关系 tables; also accepts 关系锚点 / 与主角关系 prose via books.ParseCastRelations.
func extractOnStageRelations(cast []domain.CastCard, onStage []string) string {
	on := map[string]bool{}
	for _, s := range onStage {
		on[s] = true
	}
	byStem := map[string]domain.CastCard{}
	for _, c := range cast {
		byStem[c.Stem] = c
	}
	var out []string
	for _, rel := range books.ParseCastRelations(cast) {
		if !on[rel.From] || !on[rel.To] {
			continue
		}
		a := byStem[rel.From]
		b := byStem[rel.To]
		label := rel.Type
		if rel.Current != "" {
			if label != "" {
				label += " · " + rel.Current
			} else {
				label = rel.Current
			}
		}
		out = append(out, fmt.Sprintf("%s ↔ %s：%s", CleanName(a.Name), CleanName(b.Name), label))
	}
	return strings.Join(out, "\n")
}

// previousUnit returns the unit right before the current one in the volume.
func previousUnit(snap domain.BookSnapshot, unit domain.UnitInfo) *domain.UnitInfo {
	for i, u := range snap.Units {
		if u.Volume == unit.Volume && u.Index == unit.Index-1 && u.ProseRaw != "" {
			return &snap.Units[i]
		}
	}
	return nil
}

// lastUnitOfVolume returns the highest-index unit in the given volume.
func lastUnitOfVolume(snap domain.BookSnapshot, volume int) *domain.UnitInfo {
	var best *domain.UnitInfo
	for i := range snap.Units {
		u := &snap.Units[i]
		if u.Volume != volume {
			continue
		}
		if best == nil || u.Index > best.Index {
			best = u
		}
	}
	return best
}

// extractUnitBridge keeps only contract/hook + prose tail for volume handoff.
func extractUnitBridge(unit domain.UnitInfo) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("unit_id: %s", unit.UnitID))
	if unit.Title != "" {
		parts = append(parts, "title: "+unit.Title)
	}
	for _, key := range []string{"function", "payoff", "story_day", "gap_from_prev"} {
		if v := yamlScalarLocal(unit.OutlineRaw, key); v != "" && v != "null" {
			parts = append(parts, key+": "+v)
		}
	}
	if hook := mdYAMLMapish(unit.OutlineRaw, "next_hook"); hook != "" {
		parts = append(parts, "next_hook:\n"+hook)
	}
	if unit.ProseRaw != "" {
		parts = append(parts, "文末钩子：\n"+clipTail(unit.ProseRaw, 800))
	}
	return strings.Join(parts, "\n")
}

func mdYAMLMapish(raw, key string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*$`)
	loc := re.FindStringIndex(raw)
	if loc == nil {
		// inline map: next_hook: {type: x}
		if v := yamlScalarLocal(raw, key); v != "" {
			return v
		}
		return ""
	}
	body := raw[loc[1]:]
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// latestSummaryTail returns the newest volume summary content for continuity.
func latestSummaryTail(snap domain.BookSnapshot, volume int) string {
	tag := fmt.Sprintf("continuity/summaries/v%02d.md", volume)
	if s := snap.Files[tag]; strings.TrimSpace(s) != "" {
		return s
	}
	return ""
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…(truncated)"
}

func clipTail(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…(truncated)\n" + string(r[len(r)-max:])
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
