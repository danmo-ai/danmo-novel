package contextsvc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/resource/novel"
)

const ContractVersion = "novel-v1"

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
			p.MaxCalls = chapterCount // fixed budget, no dynamic append
			p.AllowOneRepair = false
		}
	case domain.TaskReview:
		p.MaxCalls = 1
		p.AllowOneRepair = false
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

func BuildWritePacket(snap domain.BookSnapshot, unit domain.UnitInfo, modelMaxOut int) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskWrite, unit.ChapterTo-unit.ChapterFrom+1)
	if modelMaxOut > 0 && modelMaxOut < 4000 && !policy.SplitByChapter && unit.ChapterTo > unit.ChapterFrom {
		policy.SplitByChapter = true
		policy.MaxCalls = unit.ChapterTo - unit.ChapterFrom + 1
		policy.AllowOneRepair = false
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
	add("unit-contract", "单元合同", unit.OutlinePath, NormalizeUnitContract(unit.OutlineRaw))
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

	// 4. 公开事实：游标 + on_stage 人物状态 + 相关伏笔，不塞整份 facts.md。
	add("facts", "公开连载事实", "continuity/facts.md", ExtractFactsForWrite(snap.Files["continuity/facts.md"], onStage, unit.UnitID))

	// 5. 上一章 / 上一单元钩子与文末。
	if prev := previousUnit(snap, unit); prev != nil {
		if tail := clipTail(prev.ProseRaw, 1200); tail != "" {
			add("prev-tail", "上单元文末钩子", prev.ProsePath, tail)
		}
	}
	if sum := latestSummaryTail(snap, unit.Volume); sum != "" {
		add("prev-summary", "最近章摘要", "continuity/summaries", clipTail(sum, 1200))
	}

	// 6. 压缩风格指纹与写作规则。
	add("style", "风格指纹", "canon/style-fingerprint.md", ExtractStyle(snap.Files["canon/style-fingerprint.md"]))
	add("writing-rules", "写作规则", "canon/writing-rules.md", clip(snap.Files["canon/writing-rules.md"], 900))

	// 7. 当前卷锁词（不展开未来秘密）。
	add("locked-terms", "锁词", "canon/locked-terms.yaml", ExtractLockedTerms(snap.Files["canon/locked-terms.yaml"], volumeID))

	genreKB := novel.GenreBullets(snap.Summary.Genre, snap.Summary.Subgenre)
	add("genre-kb", "题材写时约束", "knowledge:"+snap.Summary.Genre, genreKB)

	// 时间书写规范：把 story_day/gap 转成正文可执行的时间锚点指令。
	add("time-guidance", "时间书写规范", "knowledge:time", buildTimeGuidance(snap, unit))

	hashes := map[string]string{}
	var ctxParts []string
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
		ctxParts = append(ctxParts, fmt.Sprintf("### %s\n%s", b.Label, b.Content))
	}

	system := novel.WriteSystemPrompt()
	user := fmt.Sprintf("书：%s\n单元：%s\n请按单元合同与场面节拍一次写出全部章节正文。\n输出严格 JSON：{\"chapters\":[{\"chapter\":N,\"title\":\"...\",\"body\":\"...\"}]}\n章与章之间不要输出 ---；body 内是纯散文。\n\n## CONTEXT\n\n%s",
		snap.Summary.ID, unit.UnitID, strings.Join(ctxParts, "\n\n"))

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

func BuildOutlinePacket(snap domain.BookSnapshot, volume int, unitIDs []string) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskOutline, 0)
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
	// 细纲规划可用的公开伏笔与揭示节奏（非作者底牌细节）。
	if facts := ExtractFactsForWrite(snap.Files["continuity/facts.md"], cast, ""); facts != "" && facts != "尚无已提交公开事实。" {
		blocks = append(blocks, domain.ContextBlock{ID: "facts", Label: "公开连载事实", Source: "continuity/facts.md", Content: facts, Bytes: len(facts)})
	}
	if reveal := clip(snap.Files["canon/reveal-schedule.md"], 900); strings.TrimSpace(reveal) != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "reveal", Label: "揭示计划", Source: "canon/reveal-schedule.md", Content: reveal, Bytes: len(reveal)})
	}
	var parts []string
	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
		parts = append(parts, fmt.Sprintf("### %s\n%s", b.Label, b.Content))
	}
	system := novel.OutlineSystemPrompt()
	user := fmt.Sprintf("书：%s\n卷：%s\n为本批 proposed 单元补全细纲合同。\n输出 JSON：{\"units\":[{unitId,title,function,desire,obstacle,choice,payoff,pleasure,forbidden,wordTarget,onStage,pov,chapterFrom,chapterTo,scenes,stateDeltas,nextHook,storyDay,gapFromPrev,infoControl}]}\n不得改卷纲已定的 function / next_hook.type；scenes / nextHook / storyDay / gapFromPrev / infoControl 必须给出。\n\n## CONTEXT\n\n%s",
		snap.Summary.ID, volTag, strings.Join(parts, "\n\n"))
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
	policy := DefaultPolicy(domain.TaskPlan, 0)
	bible := snap.Files["book-bible.md"]
	blocks := []domain.ContextBlock{
		{ID: "bible", Label: "立项 bible", Source: "book-bible.md", Content: bible, Bytes: len(bible)},
	}
	system := novel.PlanSystemPrompt()
	user := fmt.Sprintf("书：%s\n题材：%s\n请规划第 %d 卷：输出 JSON {\"cast\":[{\"stem\",\"name\",\"role\",\"status\":\"candidate\",\"cardMarkdown\"}],\"bookOutlineMarkdown\",\"volumeOutlineMarkdown\"}。\n卷纲须含单元索引表与本卷人物 stem 列表。\n\n## CONTEXT\n\n### bible\n%s",
		snap.Summary.ID, snap.Summary.Genre, volume, bible)
	return domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskPlan, BookID: snap.Summary.ID,
		VolumeID: fmt.Sprintf("v%02d", volume), InputHashes: map[string]string{"book-bible.md": shortHash(bible)},
		Blocks: blocks, KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2, Policy: policy,
		SystemPrompt: system, UserPrompt: user,
	}
}

func BuildReviewPacket(snap domain.BookSnapshot, unit domain.UnitInfo, localFindings []domain.ReviewFinding) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskReview, 0)
	prose := unit.ProseRaw
	onStage := extractOnStage(unit.OutlineRaw)
	blocks := []domain.ContextBlock{
		{ID: "outline", Label: "单元合同", Source: unit.OutlinePath, Content: unit.OutlineRaw, Bytes: len(unit.OutlineRaw)},
		{ID: "prose", Label: "正文", Source: unit.ProsePath, Content: prose, Bytes: len(prose)},
	}
	// 审校读取公开连载事实与 on_stage 人物状态；绝不读作者底牌。
	if facts := ExtractFactsForWrite(snap.Files["continuity/facts.md"], onStage, unit.UnitID); facts != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "facts", Label: "公开连载事实", Source: "continuity/facts.md", Content: facts, Bytes: len(facts)})
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
	var fb strings.Builder
	for _, f := range localFindings {
		fmt.Fprintf(&fb, "- [%s] %s: %s\n", f.Severity, f.Title, f.Evidence)
	}
	var ctxParts []string
	hashes := map[string]string{unit.ProsePath: shortHash(prose)}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
		ctxParts = append(ctxParts, fmt.Sprintf("### %s\n%s", b.Label, b.Content))
	}
	system := novel.ReviewSystemPrompt()
	user := fmt.Sprintf("书：%s 单元：%s\n对正文做一次审校。输出 JSON {\"verdict\":\"PASS|FAIL\",\"findings\":[{\"id\",\"severity\",\"lens\",\"title\",\"evidence\",\"chapter\",\"suggestion\"}],\"scores\":{}}。\n本地硬门结果：\n%s\n\n## CONTEXT\n\n%s",
		snap.Summary.ID, unit.UnitID, fb.String(), strings.Join(ctxParts, "\n\n"))
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

// BuildRewriteEntityPacket builds a single-entity rewrite packet. The LLM
// rewrites one canon/setting file and returns normalized full content; accept
// writes it back. Author-lore 禁读：bible 只取公开约束，底牌不进入。
func BuildRewriteEntityPacket(snap domain.BookSnapshot, spec domain.TaskSpec, modelMaxOut int) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskRevise, 0)
	policy.MaxCalls = 2
	policy.AllowOneRepair = true
	if modelMaxOut > 0 {
		policy.MaxOutputTokens = modelMaxOut
	}
	target := spec.TargetPath
	raw := snap.Files[target]
	blocks := []domain.ContextBlock{
		{ID: "current", Label: "当前内容", Source: target, Content: raw, Bytes: len(raw)},
	}
	// 参考约束：写作规则与风格（公开，不含底牌）。
	if w := clip(snap.Files["canon/writing-rules.md"], 800); w != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "rules", Label: "写作规则", Source: "canon/writing-rules.md", Content: w, Bytes: len(w)})
	}
	if s := ExtractStyle(snap.Files["canon/style-fingerprint.md"]); s != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "style", Label: "风格指纹", Source: "canon/style-fingerprint.md", Content: s, Bytes: len(s)})
	}
	var ctxParts []string
	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.Source] = shortHash(b.Content)
		ctxParts = append(ctxParts, fmt.Sprintf("### %s\n%s", b.Label, b.Content))
	}
	system := rewriteEntitySystemPrompt(spec.EntityKind)
	instruction := spec.Instruction
	if instruction == "" {
		instruction = "在不改变既有事实与情节的前提下，重写优化这个实体：字段规范化、结构补齐、表达精简。"
	}
	user := fmt.Sprintf("书：%s\n目标文件：%s（实体类型：%s）\n作者追加指令：%s\n\n请重写该文件并返回规范化后的完整内容。只返回 JSON {\"content\":\"...\"}，content 为该文件的完整新内容（Markdown 或 YAML 原文）。\n\n## CONTEXT\n\n%s",
		snap.Summary.ID, target, spec.EntityKind, instruction, strings.Join(ctxParts, "\n\n"))
	packet := domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskRewriteEntity, BookID: snap.Summary.ID,
		InputHashes: hashes, Blocks: blocks, KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2, Policy: policy,
		SystemPrompt: system, UserPrompt: user,
	}
	if err := AssertNoAuthorLore(&packet); err != nil {
		panic(err)
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
	case "state":
		return base + "\n只规范化 YAML 字段与注释，不改状态值。"
	default:
		return base + "\n保持文件原有章节结构，字段补齐、表达精简。"
	}
}

func BuildLocalEditPacket(snap domain.BookSnapshot, sel domain.TextSelection, instruction string) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskLocalEdit, 0)
	system := novel.LocalEditSystemPrompt()
	user := fmt.Sprintf("书：%s\n路径：%s\n指令：%s\n只改选区，返回 JSON {\"replacement\":\"...\"}。\n\n## 前文\n%s\n\n## 选区\n%s\n\n## 后文\n%s",
		snap.Summary.ID, sel.Path, instruction, sel.Before, sel.Text, sel.After)
	return domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskLocalEdit, BookID: snap.Summary.ID,
		InputHashes: map[string]string{sel.Path: shortHash(sel.Text)}, KnowledgeVersion: novel.KnowledgeVersion,
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
	gap := yamlScalarLocal(raw, "gap_from_prev")
	flashback := yamlScalarLocal(raw, "flashback") == "true"
	var b strings.Builder
	b.WriteString("时间书写规则：\n")
	if day != "" && day != "null" {
		fmt.Fprintf(&b, "- 本单元开场＝故事第 %s 天。正文时间用相对锚点（第 %s 天／次日／当晚／三天后），不得写具体年月日或星期。\n", day, day)
	} else {
		b.WriteString("- 本单元故事日未定：先用相对锚点（次日／几天后／当晚），不要虚构具体日期。\n")
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

// extractOnStageRelations returns formal relation-table rows between on-stage
// stems, parsed from the 「关系 / 人物关系」 table section of each cast card.
func extractOnStageRelations(cast []domain.CastCard, onStage []string) string {
	on := map[string]bool{}
	for _, s := range onStage {
		on[s] = true
	}
	byStem := map[string]domain.CastCard{}
	for _, c := range cast {
		byStem[c.Stem] = c
	}
	seen := map[string]bool{}
	var out []string
	for _, stem := range onStage {
		card, ok := byStem[stem]
		if !ok {
			continue
		}
		section := mdSection(card.Raw, "## 关系")
		if section == "" {
			section = mdSection(card.Raw, "## 人物关系")
		}
		for _, row := range tableRows(section) {
			target := strings.Trim(row[0], "` ")
			if !on[target] || target == stem {
				continue
			}
			key := strings.Join([]string{stem, target}, "|")
			if seen[key] {
				continue
			}
			seen[key] = true
			rel := strings.Join(row[1:], " / ")
			out = append(out, fmt.Sprintf("%s ↔ %s：%s", CleanName(card.Name), target, rel))
		}
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
