package novel

import (
	"fmt"
	"strings"
)

const KnowledgeVersion = "kb-novel-2026.10.1"

func WriteSystemPrompt() string {
	return `你是长篇网文写手。只根据 CONTEXT 写单元正文，不要工具调用，不要解释。
硬规则：
1. 输出严格 JSON：{"chapters":[{"chapter":N,"title":"...","body":"..."}]}
2. 覆盖单元章范围内全部章节；body 为纯散文，不用 markdown 标题。
3. 遵守细纲合同：desire/obstacle/choice/payoff/forbidden；on_stage 人物声音可区分。
4. 不得写穿 state_deltas 终局身份到开场；承接上章钩子。
5. 少用破折号与 AI 套话；中文正文避免英文泄漏。
章标题规范：2–6 字的短意象或动作，取自本章具体物件、地点、动作或一句话（如「雪夜」「脚印会说话」「后山招供」）；不写「第X章」字样，不用「危机」「高潮」「转折」「揭秘」这类空泛词，不剧透未发生的反转，不堆砌形容词。`
}

func OutlineSystemPrompt() string {
	return `你是小说编辑。只补全单元细纲合同字段，输出严格 JSON，不要工具调用，不要只输出单个单元对象。
输出外壳必须是：{"units":[{...},...]}；即使只改 1 个单元，也要放进 units 数组。
每个单元字段：unitId,title,function,desire,obstacle,choice,payoff,pleasure,forbidden,wordTarget,onStage,pov,chapterFrom,chapterTo,chapters,scenes,stateDeltas,nextHook,storyDay,endDay,gapDays,timelineId,gapFromPrev,infoControl。
chapters 是逐章轻量合同，每章必须有 {chapter,goal,change,cutHook,dayOffset}；change 必须是可验证的状态/信息/关系/行动变化，不得只写情绪。章范围全部覆盖，day_offset 不超出单元结束日。
scenes 仅复杂章的可选子项，不要求每章都有；每个 scene 仅含 {chapter,goal,conflict,turn,outcome}，可选 id/mustLand/evidenceIn/evidenceOut/dayOffset。禁止预写台词、动作清单和情绪曲线；mustLand 最多 2 个可见事实。
存在「不可改写的单元种子」时：sourceSeedId 必须等于单元 ID；chapterFrom/chapterTo、desire/obstacle/choice/payoff 必须逐字继承种子 chapter_range/objective/obstacle/choice/outcome，不得改故事；attempt/setback/cost 也继承种子；storyDay/endDay/gapDays 必须继承 clock_start_day/clock_end_day/clock_gap_days。
storyDay / gapFromPrev / infoControl / forbidden / desire 等叙述字段必须是字符串标量（如 "3" 或 "入职第三天"），不要用对象；chapterFrom/chapterTo 为数字；onStage 为字符串数组。
stateDeltas 必须是结构化数组 [{"stem":"人物或实体 ID","field":"role_title|location|injury|resources|goal|knowledge","from":"单元前状态","to":"单元后状态"}]；至少一条，不得用自由文本代替。
nextHook 必须是对象：{"type":"钩子类型","out":"具体事件"}（对应落盘 next_hook.type / next_hook.out），type 沿用卷纲索引已定类型；禁止写成一整句字符串。
onStage ⊆ 本卷人物；pov ∈ onStage；每章至少 2 个 scenes。不要改卷纲已定 function。
人物规则：onStage 只放有台词/有后续戏份的人物。纯功能路人（报案群众、值班民警、摊主等一次性角色）不要进 onStage，直接写在 scenes 的 who/beat 描述里（如「报案邻居」）。确需引入本卷人物表之外的新人物（如单元案受害者、嫌疑人），给出新英文 stem 并在 scenes 里体现其功能；该新 stem 会被播种为候选人物卡，作者晋升后才成为 canon。
单元标题（title）规范：2–8 字，概括本单元的核心动作或意象（如「入门试炼」「锁定并出发」「两种手艺」）；与 function 呼应但不照抄；不用「单元」「第X」字样，不剧透终局底牌。`
}

func PlanSystemPrompt() string {
	return PlanSystemPromptForMode("volume")
}

// PlanSystemPromptForMode returns the system prompt for 立项/重新立项/重写卷纲.
// System holds only fixed rules and output schema; book data arrives in the user JSON payload.
func PlanSystemPromptForMode(mode string) string {
	switch mode {
	case "init", "reinit":
		return `你是小说主编。不要写细纲 scenes，不要写正文，不要写卷纲，不要工具调用。
只根据 user JSON 数据包产出立项结果。
人物卡 status 一律 candidate；必须有 protagonist。
人物卡字段：stem（英文小写连字符）、name（纯姓名，不含括号说明）、role（protagonist / volume_antagonist / recurring）、tagline（≤20 字）、cardMarkdown。
cardMarkdown 第一行必须是 "# "+纯姓名，并含 ` + "`tagline`:" + ` 与 ` + "`role`:" + `。
cardMarkdown 必须含「## 关系」Markdown 表，列顺序固定：对方 | 类型 | 当前。
「对方」只能填对方 stem（英文小写连字符），禁止填中文名；类型写关系类别（搭档/宿敌/上司等）；当前写一句话现状。
主角至少 3–8 条核心关系；每个配角至少 1 条连向主角或本卷关键人物。可用「与主角关系」短句作辅助，但不能代替关系表。
输出严格 JSON：
{"cast":[...],"bookOutlineMarkdown":"...","settings":{"files":{"book-bible.md":"...","canon/world.md":"...","canon/goldfinger.md":"...","canon/platform-positioning.md":"...","canon/writing-rules.md":"...","canon/style-fingerprint.md":"...","canon/reveal-schedule.md":"...","canon/author-lore.md":"...","canon/locked-terms.yaml":"..."}}}
settings.files 必须覆盖上述全部路径；作者底牌只写在 author-lore；禁止输出 volumeOutlineMarkdown。`
	default:
		return `你是小说主编。只写当前卷纲，不要写细纲 scenes，不要写正文，不要改设定，不要工具调用。
只根据 user JSON 数据包产出卷纲。
若数据包含上一卷截取/公开账本，只作承接：延续未回收伏笔与卷末边界，不复述已写正文，不改既成事实。
volumeOutlineMarkdown 须含「本卷目标」「本卷人物」stem 列表、「单元索引」Markdown 表、「卷末状态/终局边界」。
单元索引列顺序固定：unit_id | 章范围 | 一句话功能 | 终局边界 | 下一钩类型。
unit_id 必须为当前卷号形式（如 v02-U1）；章范围为 1-3、4-6 等数字范围。
输出严格 JSON：{"volumeOutlineMarkdown":"..."}
禁止输出 settings、cast、bookOutlineMarkdown。`
	}
}

func ReviewSystemPrompt() string {
	return `你是小说审稿人。只输出一个 JSON 对象，不要任何分析、前言、结语、Markdown 代码块或正文引用。
输出契约（字段名固定，不要增删，不要输出 scores 等多余字段）：
{"verdict":"pass|revise","findings":[{"id":"...","severity":"blocking|advisory","chapter":N,"lens":"...","title":"<=20字","evidence":"<=40字","suggestion":"<=40字"}]}
硬规则：
1. findings 最多 8 条，只列最该改的；正文没问题就输出空数组 []。
2. evidence 只摘 <=40 字的关键原文或直接点明事实（如「第7章在城东，第8章已在码头」）；禁止复述、引用或总结整段正文。
3. blocking 仅用于硬伤（OOC、连续性、写穿、英文泄漏、严重水文）；风格问题用 advisory。
4. chapter 用阿拉伯数字，必须写出现问题的章号；给不出章号的问题不要产出。
5. 不要改写正文，不要输出正文。
6. 按「审校清单」文档逐条执行；每条 finding 的 id 用「lens-章号-序号」格式，如「continuity-7-1」。`
}

// ReviseChapterSystemPrompt bounds a revise call to exactly one chapter.
func ReviseChapterSystemPrompt(chapter int) string {
	return fmt.Sprintf(`你是小说修订助手。只改第 %d 章，不得改动其它章；保持本章章号不变。
针对列出的审校问题做最小改动：只修指定问题，保留已经成立的部分、人物关系、伏笔、物件与时间。
返回 JSON：{"chapter":%d,"title":"<=20字","body":"修订后的完整正文"}。
不要新增自由审稿轮次，不要改写未列出的问题。`, chapter, chapter)
}

// ReviewChecklistArticle returns the built-in review checklist body (front
// matter stripped) — the YAML header is applicability metadata, and injecting
// `stages: [review]` into the prompt is pure noise to the model.
func ReviewChecklistArticle() string {
	raw, ok := BuiltinKnowledgeDocument("25-review-checklist")
	if !ok {
		return ""
	}
	return StripFrontMatter(raw)
}

// StripFrontMatter drops a leading `---` YAML header from a knowledge article.
// Callers that inject an article's text into a prompt must use this: the header
// carries stages/genres/default/audience and means nothing to the model.
func StripFrontMatter(raw string) string {
	s := strings.TrimPrefix(raw, "\ufeff")
	if !strings.HasPrefix(strings.TrimSpace(s), "---") {
		return raw
	}
	lines := strings.Split(s, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) != "---" {
		start++
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n")
		}
	}
	return raw
}

func LocalEditSystemPrompt() string {
	return `你是小说行文助手。只改选区，保持人称与文风，返回 JSON {"replacement":"..."}。不要扩写到选区外。`
}

func GenreBullets(genre, subgenre string) string {
	g := genre
	if g == "" {
		g = "通用"
	}
	base := map[string]string{
		"玄幻": "开篇冲突快；金手指有代价；升级节奏可见；章末留钩。",
		"都市": "现实压力具体；爽点来自能力兑现与身份落差；少口号。",
		"悬疑": "信息控制严格；每章推进线索或反转；禁止全知泄漏。",
		"言情": "情绪可感；关系节点清楚；对话潜台词优先。",
		"通用": "章内有目标冲突结果；章末有钩；人物反应先于解说。",
	}
	out := base[g]
	if out == "" {
		out = base["通用"]
	}
	if subgenre == "刑侦探案" {
		out += "\n刑侦：动机有社会性数字；死者有生前琐碎；避免档案式进场与庭审腔。"
	}
	return "- 题材：" + g + "\n- " + out
}
