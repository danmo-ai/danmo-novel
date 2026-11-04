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
	"gopkg.in/yaml.v3"
)

const ContractVersion = "novel-v1"

// 输出 token 上限不由上层决定。
//
// 曾经每类任务都在这里写死一个数（写 8192、审校 32768、立项 65536、局部改写
// 4096），client 里那套「按模型目录 max_output 决定」的逻辑永远轮不到生效。
// 结果是一个 max_output 声明 384000 的模型被按 8k 用，思考 tokens 和答案共用
// 那份预算，一个正常章节就从 JSON 中间被切断，报出来的还是"没有 JSON"。
//
// 现在一律传 0 = 交给 LLM client 按模型配置处理：目录里有 max_output 就用它，
// 没有就不发这个字段、由 provider 用自己默认值。是否被截断改看响应协议里的
// finish_reason，而不是猜"有没有正好用满某个数"。
func DefaultPolicy(kind domain.TaskKind, chapterCount int) domain.CallPolicy {
	p := domain.CallPolicy{
		MaxCalls:       2, // generate + optional one structure repair
		TimeoutSec:     600,
		AllowOneRepair: true,
	}
	switch kind {
	case domain.TaskWrite:
		// 正文固定逐章生成、逐章定向修复，单元 Accept 仍一次提交全部章节。
		p.MaxCalls = chapterCount * 3
	case domain.TaskReview:
		p.MaxCalls = 2
		p.AllowOneRepair = true
	case domain.TaskRevise, domain.TaskFinalize:
		p.MaxCalls = 1
		p.AllowOneRepair = false
	case domain.TaskLocalEdit, domain.TaskContinue, domain.TaskExpand, domain.TaskPolish, domain.TaskRewriteSel:
		p.MaxCalls = 1
		p.AllowOneRepair = false
	}
	return p
}

// withUserInstruction appends the author's 追加指令 to the stage's default
// instruction. The default text carries the output-format and gate contract, so
// it is never replaced — the author's wording is layered on top as 补充要求.
// 所有入口走同一个 builder，因此这个字段的语义完全一致。
func withUserInstruction(base, instruction string) string {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return base
	}
	return base + "\n\n补充要求（作者追加；不得违反上面的输出格式与门禁约束）：\n" + instruction
}

func ValidateUnitInput(snap domain.BookSnapshot, unit domain.UnitInfo) error {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(unit.OutlineRaw), &doc); err != nil {
		return fmt.Errorf("细纲 YAML 无效：%w", err)
	}
	if len(extractOnStage(unit.OutlineRaw)) == 0 || extractPOV(unit.OutlineRaw) == "" {
		return fmt.Errorf("细纲缺少 on_stage/pov，不能生成正文")
	}
	if _, err := books.LoadState(snap.Summary.RootPath); err != nil {
		return fmt.Errorf("连续性账本不可读：%w", err)
	}
	return nil
}

func BuildWritePacket(snap domain.BookSnapshot, unit domain.UnitInfo, instruction string) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskWrite, unit.ChapterTo-unit.ChapterFrom+1)

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
	//
	// 不再调用 NormalizeUnitContract：它会追加一段「# 本单元故事时钟
	// story_day=67 · gap_from_prev=1」，而同一组数字已经由 time-guidance 用
	// 可执行的散文说过一遍、合同原始字段又说一遍。时钟只保留一处。
	add("unit-contract", "单元合同", unit.OutlinePath, TrimContract(ContractWithoutBeats(unit.OutlineRaw), domain.TaskWrite))
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
	//
	// 上一章尾部由 RunWrite 按章放进 extra，保持公共 documents 稳定。
	if unit.ChapterFrom <= 1 || previousUnit(snap, unit) == nil {
		if sum := latestSummaryTail(snap, unit.Volume, unit.UnitID); sum != "" {
			add("prev-summary", "最近章摘要", "continuity/actual", clipTail(sum, 1200))
		}
	}

	// 6. 风格与写作规则合成一块：两者都是"怎么写"，分开注入等于让模型读两遍同一类约束。
	add("style-rules", "风格与写作规则", "canon/style-fingerprint.md", mergeStyleRules(snap))

	// 7. 当前卷锁词（不展开未来秘密）。
	add("locked-terms", "锁词", "canon/locked-terms.yaml", ExtractLockedTerms(snap.Files["canon/locked-terms.yaml"], volumeID))

	// 7b. 能力上限：金手指与世界规则的代价、上限、例外。
	// 此前写作层完全没有这块，能力只能靠 models 自觉不越界。
	add("ability-limits", "能力上限", "canon/goldfinger.md", ExtractAbilityLimits(snap.Files["canon/world.md"], snap.Files["canon/goldfinger.md"]))

	// 7c. 伏笔由 RunWrite 按章放进 extra。

	genreKB := novel.GenreBullets(snap.Summary.Genre, snap.Summary.Subgenre)
	add("genre-kb", "题材写时约束", "knowledge:"+snap.Summary.Genre, genreKB)
	// 时间书写规范：把 story_day/gap 转成正文可执行的时间锚点指令。
	// 它是时钟的唯一出处：合同里的 story_day/end_day/gap_days 已从写作包里裁掉，
	// 否则同一组数字会以原始字段、追加段、散文说明三种形态各出现一次。
	add("time-guidance", "时间书写规范", "knowledge:time", buildTimeGuidance(snap, unit))

	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.ID+"|"+b.Source] = shortHash(b.Content)
	}

	system := novel.WriteChapterSystemPromptFor(books.ChapterTarget(unit))
	baseInstruction := "请按本章合同与场面节拍只写 user 数据包 chapter 指定的那一章。章内不要输出 ---；body 内是纯散文。"
	user := BuildUserPrompt("write", "", map[string]any{
		"id": snap.Summary.ID, "unitId": unit.UnitID, "volumeId": volumeID,
	}, withUserInstruction(baseInstruction, instruction), blocks, nil)

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
//
// 写作规则按小节过滤：写单章时「单元 3–8 章，硬上限 10 章」「每 3–5 章交付一次
// 结果型兑现」「单章默认 2300—2700 字」都不属于本章可执行的动作，最后一条还
// 与 system 的 1800–3500 区间打架。只按 `## ` 分节整块丢弃，不做逐句正则——
// 误删一条章节级规则比多留一条单元级规则更难发现。
func mergeStyleRules(snap domain.BookSnapshot) string {
	style := ExtractStyle(snap.Files["canon/style-fingerprint.md"])
	rules := clip(filterChapterScopedRules(snap.Files["canon/writing-rules.md"]), 420)
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

// filterChapterScopedRules 丢弃「单元」「卷」级的小节，只留单章可执行的条目。
func filterChapterScopedRules(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var out []string
	var section []string
	keep := true
	flush := func() {
		if len(section) == 0 || !keep {
			section = nil
			return
		}
		out = append(out, section...)
		section = nil
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			keep = !strings.Contains(heading, "单元") && !strings.Contains(heading, "卷")
		}
		section = append(section, line)
	}
	flush()
	return strings.TrimSpace(strings.Join(out, "\n"))
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
	// chapter_chars 由 system 提示词与每次调用的 chapterRule 给出，且
	// ChapterTarget 会把它 Clamp 到 [2000,3500]。合同里若写着 5000，
	// 模型会看到两个互相矛盾的数字。
	"chapter_chars",
	// 时钟四件套由 time-guidance 统一表达成可执行指令（开场第几天、最晚第几天、
	// 与上一单元至少隔几天）。原始字段留在合同里只是第三份同样的数字。
	"story_day",
	"end_day",
	"gap_days",
	"gap_from_prev",
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

// outlineSeedWindows 取本次请求单元的章范围，用于裁剪卷纲里的卷级节拍。
// 没有指定单元（整卷细纲）时返回空，调用方按「全部保留」处理。
func outlineSeedWindows(doc domain.StructuredMarkdown, unitIDs []string) [][2]int {
	want := map[string]bool{}
	for _, id := range unitIDs {
		want[strings.TrimSpace(id)] = true
	}
	if len(want) == 0 {
		return nil
	}
	var out [][2]int
	for _, e := range doc.EntitiesOf("单元") {
		if !want[e.ID] {
			continue
		}
		if from, to := books.ChapterWindow(e.Value("chapter_range")); from > 0 && to >= from {
			out = append(out, [2]int{from, to})
		}
	}
	return out
}

// outlineSeedContent renders only the seeds this run can actually rewrite.
//
// 以前是 `json.Marshal(doc.EntitiesOf("单元"))`：请求 1 个单元也灌整卷，且每个
// 字段都带 {line,column,byteOffset} 源码位置。实测 v01-U2 的种子块 28,245 字符，
// 其中 58% 是 range、87% 属于别的单元，真正的合同只有 290 字。模型要在两万字
// 噪声里找 21 个必须「逐字继承」的字段，漏字段几乎是必然的。
//
// 输出形态也从 [{kind,id,fields:[{name,value,range}]}] 压平成
// [{kind,id,chapter_range,objective,...}]：字段名保持卷纲 Markdown 里的原样，
// 因为提示词就是按这些名字要求逐字继承的。
func outlineSeedContent(doc domain.StructuredMarkdown, unitIDs []string) (string, bool) {
	want := map[string]bool{}
	for _, id := range unitIDs {
		want[strings.TrimSpace(id)] = true
	}
	seeds := []map[string]any{}
	for _, e := range doc.EntitiesOf("单元") {
		if len(want) > 0 && !want[e.ID] {
			continue
		}
		seed := map[string]any{"kind": e.Kind, "id": e.ID}
		for _, f := range e.Fields {
			seed[f.Name] = f.Value
		}
		seeds = append(seeds, seed)
	}
	if len(seeds) == 0 {
		return "", false
	}
	raw, err := json.Marshal(seeds)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// outlineIdentityContent 只给「在本次单元边界生效」的身份变更。
//
// 身份段决定 on_stage 里谁有生效的公开身份（UnitContractForSeed 用 IdentityAt
// 过滤），所以细纲必须看得见它；但它是独立实体、字段与单元种子完全不同，混进
// 种子块会把「逐字继承」的对象搞混，因此单独成块。
func outlineIdentityContent(doc domain.StructuredMarkdown, unitIDs []string) (string, bool) {
	want := map[string]bool{}
	for _, id := range unitIDs {
		want[strings.TrimSpace(id)] = true
	}
	if len(want) == 0 {
		return "", false
	}
	var out []map[string]any
	for _, e := range doc.EntitiesOf("身份变更") {
		if !want[e.Value("from_unit")] {
			continue
		}
		item := map[string]any{"id": e.ID}
		for _, f := range e.Fields {
			item[f.Name] = f.Value
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return "", false
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// outlineVolumeForUnits 把卷纲裁成「写这几个单元」真正需要的部分。
//
// 保留：卷级头部（goal/central_conflict/stakes/entry_state/exit_state/cast）、
// 章窗与请求单元相交的 `## 节拍`、以及卷末交接这类卷级段落。
// 丢弃：`## 单元` 区段——它与 seed block 同源同内容（同一次解析、同一份文件）。
func outlineVolumeForUnits(raw string, windows [][2]int) string {
	if strings.TrimSpace(raw) == "" || len(windows) == 0 {
		return raw
	}
	var out []string
	var section []string
	flush := func() {
		if len(section) == 0 {
			return
		}
		if outlineVolumeSectionKept(section, windows) {
			out = append(out, section...)
		}
		section = nil
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
		}
		section = append(section, line)
	}
	flush()
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func outlineVolumeSectionKept(section []string, windows [][2]int) bool {
	heading := strings.TrimSpace(section[0])
	switch {
	case !strings.HasPrefix(heading, "## "):
		return true // 卷级头部：本卷目标、冲突、起终状态
	case strings.HasPrefix(heading, "## 单元"):
		return false
	case strings.HasPrefix(heading, "## 节拍"):
		from, to := books.ChapterWindow(outlineFieldValue(section, "chapter_window"))
		if from <= 0 || to < from {
			return true // 读不出章范围就不敢删
		}
		for _, w := range windows {
			if from <= w[1] && to >= w[0] {
				return true
			}
		}
		return false
	default:
		return true // 卷末交接等卷级段落
	}
}

// outlineCastStems 决定细纲要看哪几张人物卡。
//
// 优先用请求单元的 on_stage（作者已确认的在场名单）；没有任何细纲头时退回
// 卷人物表。两种情况下都保持卷人物表的相对顺序，前缀缓存才不会因为单元顺序
// 抖动而失效。
func outlineCastStems(snap domain.BookSnapshot, unitIDs []string, volumeCast []string) []string {
	onStage := map[string]bool{}
	for _, id := range unitIDs {
		for _, u := range snap.Units {
			if u.UnitID != id {
				continue
			}
			for _, stem := range extractOnStage(u.OutlineRaw) {
				onStage[stem] = true
			}
		}
	}
	if len(onStage) == 0 {
		return volumeCast
	}
	var out []string
	for _, stem := range volumeCast {
		if onStage[stem] {
			out = append(out, stem)
		}
	}
	// on_stage 里可能有卷人物表之外的 stem（新引入的单元案角色）；它们还没
	// 人物卡，加进来也只会得到空块，所以照旧由 gates 报 advisory。
	if len(out) == 0 {
		return volumeCast
	}
	return out
}

// outlineFieldValue 读一个 Markdown 小节里的 `- key: value`。
func outlineFieldValue(section []string, key string) string {
	for _, line := range section {
		s := strings.TrimSpace(line)
		if !strings.HasPrefix(s, "- "+key) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(s, "- "+key))
		if !strings.HasPrefix(rest, ":") && !strings.HasPrefix(rest, "：") {
			continue
		}
		rest = strings.TrimPrefix(rest, ":")
		rest = strings.TrimPrefix(rest, "：")
		return strings.TrimSpace(rest)
	}
	return ""
}

func firstOutlineUnit(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func BuildOutlinePacket(snap domain.BookSnapshot, volume int, unitIDs []string, instruction string) domain.ContextPacket {
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
	volDoc, volStructured := books.StructuredOutlineFor(snap, "outline/volumes/"+volTag+".md")
	var seedDoc domain.StructuredMarkdown
	if volStructured && volDoc.Valid() {
		seedDoc = volDoc
	}
	// 卷纲只留「这一次要写的单元」用得上的部分：卷级头部、章窗相交的卷级节拍、
	// 卷末交接。单元区段整段丢弃——它与下面的种子块同源同内容，留着等于把同一份
	// 合同注入两遍，还多一份散文格式让模型在两种表述之间对不齐。
	//
	// 以前这里注入卷纲全文：实测重写 v01-U2（第 7–10 章）时，上下文里躺着
	// chapter_window 1-6 / 39-45 / 46-50 三个别的单元的节拍，等于在邀请模型
	// 把别人的事件写进本单元。
	volTrimmed := outlineVolumeForUnits(volRaw, outlineSeedWindows(seedDoc, unitIDs))
	blocks := []domain.ContextBlock{
		{ID: "volume", Label: "卷纲", Source: "outline/volumes/" + volTag + ".md", Content: volTrimmed, Bytes: len(volTrimmed)},
	}
	if seedRaw, ok := outlineSeedContent(seedDoc, unitIDs); ok {
		blocks = append(blocks, domain.ContextBlock{ID: "unit-seeds", Label: "不可改写的单元种子", Source: "outline/volumes/" + volTag + ".md#units", Content: seedRaw, Bytes: len(seedRaw)})
	}
	if idRaw, ok := outlineIdentityContent(seedDoc, unitIDs); ok {
		blocks = append(blocks, domain.ContextBlock{ID: "unit-identity", Label: "本单元生效的身份变更", Source: "outline/volumes/" + volTag + ".md#identity", Content: idRaw, Bytes: len(idRaw)})
	}
	for _, id := range unitIDs {
		for _, u := range snap.Units {
			if u.UnitID == id {
				owned := ContractWithoutBeats(u.OutlineRaw)
				owned = dropTopLevelKeys(owned, []string{"source_seed_id", "function", "desire", "obstacle", "choice", "payoff", "attempt", "setback", "cost", "story_day", "end_day", "gap_days", "state_deltas", "state_changes", "entry_cause", "exit_result", "thread_ids", "event_ids", "clock_deadline", "chapter_range"})
				blocks = append(blocks, domain.ContextBlock{
					ID: "head-" + id, Label: "细纲·可编辑子级字段", Source: u.OutlinePath, Content: owned, Bytes: len(owned),
				})
			}
		}
	}
	// 人物卡只给本单元真正在场的人：已有细纲头的 on_stage 是作者确认过的名单，
	// 直接摊开整卷人物表会让 11 张卡（每张上千字）把合同挤出注意力。
	// 没有细纲头（新单元）时退回卷人物表——那时 on_stage 还没有人定过。
	for _, stem := range outlineCastStems(snap, unitIDs, cast) {
		for _, c := range snap.Cast {
			if c.Stem != stem {
				continue
			}
			body := clip(c.Raw, 1200)
			blocks = append(blocks, domain.ContextBlock{
				ID: "cast-" + stem, Label: "人物·" + CleanName(c.Name), Source: "canon/cast/" + stem + ".md", Content: body, Bytes: len(body),
			})
		}
	}
	// 细纲规划需要的连续性：重写已有单元时要知道人物当前处境，避免与已定稿前后矛盾。
	if cont := ExtractContinuityForUnit(snap, unitIDs, outlineCastStems(snap, unitIDs, cast), ContinuityForReview); cont != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "continuity", Label: "连续性状态", Source: books.StatePath, Content: cont, Bytes: len(cont)})
	}
	if reveal := ForeshadowPlanningEntities(snap, volTag, firstOutlineUnit(unitIDs)); strings.TrimSpace(reveal) != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "foreshadows", Label: "伏笔登记表（相关完整条目）", Source: books.ForeshadowSchedulePath, Content: reveal, Bytes: len(reveal)})
	}
	if ability := ExtractAbilityLimits(snap.Files["canon/world.md"], snap.Files["canon/goldfinger.md"]); ability != "" {
		blocks = append(blocks, domain.ContextBlock{ID: "ability-limits", Label: "能力上限", Source: "canon/goldfinger.md", Content: ability, Bytes: len(ability)})
	}
	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.ID+"|"+b.Source] = shortHash(b.Content)
	}
	system := novel.OutlineSystemPrompt()
	instr := withUserInstruction("为 unitIds 列出的单元生成/重写细纲合同，输出 {\"units\":[...]}（即使只有 1 个单元也必须用数组）。不得改卷纲已定的 next_hook.type；function 可更具体但不得为空；scenes / nextHook / storyDay / gapFromPrev / infoControl 必须给出。已有细纲头时在其基础上补全或优化，不要返回空 units。", instruction)
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

// BuildIdeaPacket builds the context for 创意扩写（立项之前）。
//
// 只读作者意图：立项输入、bible、平台定位、风格指纹。不读已接受的总纲/人物，
// 因为在首次立项时它们还不存在；重新扩写时也不该让旧总纲把方向锁死——
// 换方向正是这一步存在的理由。
func BuildIdeaPacket(snap domain.BookSnapshot, instruction string) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskIdea, 0)
	blocks := []domain.ContextBlock{}
	hashes := map[string]string{}
	add := func(id, label, source string) {
		content := strings.TrimSpace(snap.Files[source])
		if content == "" {
			return
		}
		blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
		hashes[source] = shortHash(content)
	}
	add("idea", "立项输入与现有创意提案", domain.IdeaPath)
	add("bible", "作者意图 bible", "book-bible.md")
	add("platform", "平台定位", "canon/platform-positioning.md")
	add("style", "风格指纹", "canon/style-fingerprint.md")

	bookMeta := map[string]any{
		"id": snap.Summary.ID, "title": snap.Summary.Title,
		"genre": snap.Summary.Genre, "subgenre": snap.Summary.Subgenre,
	}
	inst := strings.TrimSpace(instruction)
	if inst == "" {
		inst = "把立项输入扩成一份可立项的创意提案：明确推荐主线，并覆盖一句话故事、读者承诺、主角、对抗力量、世界规则、开篇钩子、转折与结局方向。"
	}
	system := novel.IdeaExpandSystemPrompt()
	user := BuildUserPrompt("idea", "", bookMeta, inst, blocks, nil)
	return domain.ContextPacket{
		ContractVersion: ContractVersion, TaskKind: domain.TaskIdea, BookID: snap.Summary.ID,
		InputHashes: hashes, Blocks: blocks, KnowledgeVersion: novel.KnowledgeVersion,
		EstPromptTokens: (len([]rune(system)) + len([]rune(user))) / 2, Policy: policy,
		SystemPrompt: system, UserPrompt: user,
	}
}

// BuildPlanPacketWithMode builds the context for 立项 / 重新立项 / 重写卷纲.
func structuredFields(entity domain.MarkdownEntity) map[string]string {
	out := map[string]string{"id": entity.ID}
	for _, field := range entity.Fields {
		out[field.Name] = field.Value
	}
	return out
}

func bookOutlineProjection(snap domain.BookSnapshot) string {
	doc, ok := books.StructuredOutlineFor(snap, "outline/book_outline.md")
	if !ok {
		return snap.Files["outline/book_outline.md"]
	}
	lines := []string{"- logline: " + doc.Root.Value("logline")}
	for _, thread := range doc.EntitiesOf("故事线") {
		lines = append(lines, "- "+thread.ID+"："+thread.Value("opening_state")+"→"+thread.Value("ending_state"))
	}
	return strings.Join(lines, "\n")
}

func BuildPlanPacketWithMode(snap domain.BookSnapshot, volume int, mode, instruction string) domain.ContextPacket {
	if mode == "" {
		if strings.TrimSpace(snap.Files["outline/book_outline.md"]) == "" {
			mode = domain.PlanModeInit
		} else {
			mode = domain.PlanModeVolume
		}
	}
	policy := DefaultPolicy(domain.TaskPlan, 0)
	full := mode == domain.PlanModeInit || mode == domain.PlanModeReinit

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
	addFile("idea", "作者已确认的创意提案（第一依据）", domain.IdeaPath)
	addFile("bible", "作者意图 bible", "book-bible.md")
	if full {
		if mode == domain.PlanModeReinit {
			// 重新立项要把已接受的总纲派生层作为「现状」交给模型，它才知道
			// 哪些事实已经被作者确认、不能凭空改写。
			addFile("world", "世界观", "canon/world.md")
			addFile("goldfinger", "金手指", "canon/goldfinger.md")
			addFile("platform", "平台定位", "canon/platform-positioning.md")
			addFile("rules", "写作规则", "canon/writing-rules.md")
			addFile("style", "风格指纹", "canon/style-fingerprint.md")
			addFile("foreshadows", "伏笔登记表", books.ForeshadowSchedulePath)
			addFile("author-lore", "作者底牌", "canon/author-lore.md")
			addFile("locked", "锁词", "canon/locked-terms.yaml")
			addFile("book-outline", "全书总纲", "outline/book_outline.md")
			for _, c := range snap.Cast {
				src := "canon/cast/" + c.Stem + ".md"
				blocks = append(blocks, domain.ContextBlock{ID: "cast-" + c.Stem, Label: "人物·" + CleanName(c.Name), Source: src, Content: clip(c.Raw, 1800), Bytes: len(clip(c.Raw, 1800))})
				hashes[src] = shortHash(c.Raw)
			}
		} else {
			// 首次立项只给作者意图层（idea / bible / 平台 / 风格）。
			// 世界、金手指、揭示计划、底牌这些文件此刻要么不存在，要么是
			// 脚手架占位符——把它们当既有 canon 注入，模型会把「- 谁能做什么：」
			// 这种空模板当成需要保留的设定，反而写不出真正的故事。
			addFile("platform", "平台定位", "canon/platform-positioning.md")
			addFile("style", "风格指纹", "canon/style-fingerprint.md")
		}
	}
	volID := ""
	bookMeta := map[string]any{"id": snap.Summary.ID, "title": snap.Summary.Title, "genre": snap.Summary.Genre, "subgenre": snap.Summary.Subgenre}
	if !full {
		if projected := bookOutlineProjection(snap); projected != "" {
			blocks = append(blocks, domain.ContextBlock{ID: "book-outline", Label: "总纲·故事线与必要全局约束", Source: "outline/book_outline.md", Content: projected, Bytes: len(projected)})
			hashes["outline/book_outline.md"] = shortHash(projected)
		}
		if volume < 1 {
			volume = 1
		}
		volID = fmt.Sprintf("v%02d", volume)
		bookMeta["volumeId"] = volID
		if doc, ok := books.StructuredOutlineFor(snap, "outline/book_outline.md"); ok && doc.Valid() {
			if arc, found := doc.Entity("分卷", volID); found {
				if arcRaw, err := json.Marshal(structuredFields(arc)); err == nil {
					blocks = append(blocks, domain.ContextBlock{ID: "parent-volume-arc", Label: "不可改写的总纲分卷合同", Source: "outline/book_outline.md#" + volID, Content: string(arcRaw), Bytes: len(arcRaw)})
					hashes["outline/book_outline.md#"+volID] = shortHash(string(arcRaw))
				}
			}
		}
		if reveal := ForeshadowPlanningEntities(snap, volID, ""); strings.TrimSpace(reveal) != "" {
			blocks = append(blocks, domain.ContextBlock{ID: "foreshadows", Label: "伏笔登记表", Source: books.ForeshadowSchedulePath, Content: reveal, Bytes: len(reveal)})
			hashes[books.ForeshadowSchedulePath] = shortHash(reveal)
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
			if sum := latestSummaryTail(snap, volume-1, ""); strings.TrimSpace(sum) != "" {
				tail := clipTail(sum, 1500)
				src := "continuity/actual/" + prevID
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
			inst = "首次立项：以「创意提案」为第一依据，生成全书总纲、全部设定与人物。不要写卷纲。提案给了多个方向时采用推荐主线；作者追加指令指定的方向优先。"
		case domain.PlanModeReinit:
			inst = "重新立项：以「创意提案」为第一依据，在保留故事骨架前提下，重写总纲、全部设定与人物。不要写卷纲，不触碰已有单元/正文。"
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

func BuildOutlineReviewPacket(snap domain.BookSnapshot, unit domain.UnitInfo) domain.ContextPacket {
	base := BuildOutlinePacket(snap, unit.Volume, []string{unit.UnitID}, "")
	blocks := append([]domain.ContextBlock(nil), base.Blocks...)
	blocks = append(blocks, domain.ContextBlock{ID: "outline-target", Label: "待确认完整细纲", Source: unit.OutlinePath, Content: unit.OutlineRaw, Bytes: len(unit.OutlineRaw)})
	user := BuildUserPrompt("outline_review", "", map[string]any{"id": snap.Summary.ID, "unitId": unit.UnitID}, "检查这份细纲能否直接执行正文；只输出问题，不改写。", blocks, nil)
	return domain.ContextPacket{ContractVersion: ContractVersion, TaskKind: domain.TaskOutlineReview, BookID: snap.Summary.ID, UnitID: unit.UnitID, VolumeID: base.VolumeID, Blocks: blocks, Policy: DefaultPolicy(domain.TaskOutlineReview, 1), SystemPrompt: novel.OutlineReviewSystemPrompt(), UserPrompt: user, EstPromptTokens: len([]rune(user)) / 2}
}

// BuildRewriteEntityPacket builds a single-entity rewrite packet.
// 总纲派生设定（人物/揭示/世界/底牌等）必须携带总纲与相关结构作依据；
// 工艺类只带规则与风格。Accept 只写目标文件，依据只读。
func BuildRewriteEntityPacket(snap domain.BookSnapshot, spec domain.TaskSpec) domain.ContextPacket {
	policy := DefaultPolicy(domain.TaskRevise, 0)
	policy.MaxCalls = 2
	policy.AllowOneRepair = true
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
		hashes[b.ID+"|"+b.Source] = shortHash(b.Content)
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
	var previous *domain.UnitInfo
	for i := range snap.Units {
		u := &snap.Units[i]
		if u.UnitID == unit.UnitID || u.ProseRaw == "" || books.UnitOrderKey(u.UnitID) >= books.UnitOrderKey(unit.UnitID) {
			continue
		}
		if previous == nil || books.UnitOrderKey(u.UnitID) > books.UnitOrderKey(previous.UnitID) {
			previous = u
		}
	}
	return previous
}

// SummaryBeforeUnit reads only accepted summaries strictly before the target unit.
func SummaryBeforeUnit(snap domain.BookSnapshot, unit domain.UnitInfo) string {
	return latestSummaryTail(snap, unit.Volume, unit.UnitID)
}

// lastUnitOfVolume returns the highest-index unit in the given volume.
func lastUnitOfVolume(snap domain.BookSnapshot, volume int) *domain.UnitInfo {
	var best *domain.UnitInfo
	for i := range snap.Units {
		u := &snap.Units[i]
		if u.Volume != volume || (u.Status != domain.UnitReviewed && u.Status != domain.UnitFinalized) {
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

// latestSummaryTail reads the actual-ledger projection, never a second fact file.
// Legacy summaries are only used for historical chapters with no actual index.
func latestSummaryTail(snap domain.BookSnapshot, volume int, beforeUnitID string) string {
	if volume < 1 {
		return ""
	}
	if snap.Summary.RootPath != "" {
		summary, err := books.ProjectVolumeSummary(snap.Summary.RootPath, volume, beforeUnitID)
		if err == nil && summary != "" {
			return summary
		}
	}
	// Legacy-only in-memory snapshots are still readable during migration.
	return snap.Files[fmt.Sprintf("continuity/summaries/v%02d.md", volume)]
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
