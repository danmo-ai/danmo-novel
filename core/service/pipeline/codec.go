package pipeline

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/prompt"
)

const bookProtocolSpec = `只输出一个结构化 Markdown 总纲文档。根字段包含 schema_version/book_id/title/logline。
用「## 承诺 · id」「## 故事线 · id」「## 分卷 · v01」及「### 转折 · event-id」声明实体。
分卷必填 title/chapter_range/purpose/dramatic_question/central_conflict/stakes/entry_state/exit_state/thread_moves/deliverables/open_questions/reveal_ids/forbidden_reveal_ids/next_volume_entry。
reveal_ids 与 forbidden_reveal_ids 只填揭示计划里的 ID（如 r01,r02）；没有要禁揭或揭示的内容时把该行留空，不要写「无」或括号解释。
转折必填 event/cause/effect/chapter_window。下层不得擅自改变父级合同。`

const volumeProtocolSpec = `只输出一个结构化 Markdown 卷纲文档。根字段包含 schema_version/volume_id/title/source_arc_id/chapter_range/goal/central_conflict/stakes/entry_state/exit_state/cast。
用「## 节拍 · id」「## 单元 · v01-U1」「## 卷末交接 · handoff」声明实体。
单元必填 title/chapter_range/objective/obstacle/attempt/setback/choice/cost/outcome/entry_cause/exit_result/state_changes/thread_ids/event_ids/reveal_ids/forbidden_reveal_ids/next_hook。
单元必须一个主要戏剧问题、一个主结果、一种主要冲突形态；从属线必须改变主线选择或代价，不能拼接独立任务。
每单元附 clock_start_day/clock_end_day（相对故事日整数）和 clock_gap_days（与上一单元的最少间隔天数）；可选 clock_deadline 为“事件ID@故事日”，timeline_id 默认为 main。
reveal_ids 与 forbidden_reveal_ids 只填揭示计划里的 ID（如 r01,r02）；没有则留空，不要写「无」或括号解释。
节拍/转折必填 event/cause/effect/chapter_window；至少一个高潮与不可逆结果。
职务/机构变化在所属单元后添加「### 身份变更 · 单元ID-人物stem」，含 stem/role_title/org/from_unit/reason，from_unit 等于所属单元 ID。`

const bookProtocolTemplate = `# 全书总纲 · {{book.title}}
- schema_version: 1
- book_id: {{book.id}}
- title: {{书名}}
- logline: {{主角＋困境＋目标＋核心冲突}}

## 承诺 · {{promise-id}}
- description: {{持续兑现的阅读体验}}

## 故事线 · {{thread-id}}
- kind: {{main|character|relationship|world|mystery|other}}
- description: {{这条线的内容}}
- opening_state: {{起点}}
- ending_state: {{终点}}

## 分卷 · {{v01}}
- arc_id: {{arc-v01}}
- title: {{本卷标题}}
- chapter_range: {{1}}-{{50}}
- purpose: {{本卷结构职责}}
- dramatic_question: {{本卷必须回答的问题}}
- central_conflict: {{核心冲突}}
- stakes: {{失败的代价}}
- entry_state: {{开卷事实}}
- exit_state: {{卷末不可逆状态}}
- thread_moves: {{thread-id=从什么状态->到什么状态}}
- deliverables: {{必须兑现的结果}}
- open_questions: {{仍保留的问题}}
- reveal_ids: {{可揭示 ID，如 r01,r02；没有则该行留空}}
- forbidden_reveal_ids: {{禁止提前揭示 ID，如 r11,r15；没有则该行留空}}
- next_volume_entry: {{下一卷行动入口}}

### 转折 · {{event-id}}
- event: {{具体事件}}
- cause: {{由哪个前序行动导致}}
- effect: {{改变什么后续行动或状态}}
- chapter_window: {{1}}-{{10}}`

const volumeProtocolTemplate = `# 卷纲 · {{v01}}
- schema_version: 1
- volume_id: {{v01}}
- title: {{本卷标题}}
- source_arc_id: {{arc-v01}}
- chapter_range: {{1}}-{{50}}
- goal: {{总纲锁定的卷级问题}}
- central_conflict: {{核心冲突}}
- stakes: {{失败的代价}}
- entry_state: {{开卷事实}}
- exit_state: {{卷末不可逆状态}}
- cast: {{人物 stem 列表}}

## 节拍 · {{event-id}}
- event: {{具体事件}}
- cause: {{前序原因}}
- effect: {{不可逆结果}}
- chapter_window: {{1}}-{{6}}

## 单元 · {{v01-U1}}
- title: {{单元标题}}
- chapter_range: {{1}}-{{6}}
- objective: {{主角要什么}}
- obstacle: {{具体阻碍}}
- attempt: {{尝试的方法}}
- setback: {{方法的缺口}}
- choice: {{主角作出的关键选择}}
- cost: {{选择承担的代价}}
- outcome: {{可见的不可逆结果}}
- entry_cause: {{前序单元结果或钩子 ID}}
- exit_result: {{本单元离场状态}}
- state_changes: {{目标.字段=原状态->新状态}}
- clock_start_day: {{本单元开场相对故事日整数}}
- clock_end_day: {{本单元结束相对故事日整数，不早于开场}}
- clock_gap_days: {{距上一单元的最少间隔天数}}
- clock_deadline: {{可选：关键事件ID@死线故事日}}
- timeline_id: {{main 或闪回/并行线 ID}}
- allow_flashback: {{非 main 时间线时为 true，否则省略}}
- thread_ids: {{故事线 ID}}
- event_ids: {{承接的节拍 ID}}
- reveal_ids: {{可揭示 ID，如 r01,r02；没有则留空}}
- forbidden_reveal_ids: {{禁揭 ID，如 r11,r15；没有则留空}}
- next_hook: {{钩子类型 | 具体未决事件}}

## 卷末交接 · {{handoff}}
- deliverables: {{本卷已兑现}}
- open_questions: {{未兑现承诺}}
- next_volume_entry: {{下一卷行动入口}}`

var bookOutlineContract = prompt.NewContract(books.BookOutlineProtocol, "markdown", bookProtocolSpec, bookProtocolTemplate)
var volumeOutlineContract = prompt.NewContract(books.VolumeOutlineProtocol, "markdown", volumeProtocolSpec, volumeProtocolTemplate)

type markdownCodec struct {
	contract OutputProtocol
	kind     string
}

func (c markdownCodec) Protocol() OutputProtocol { return c.contract }
func (c markdownCodec) Decode(raw string) (domain.StructuredMarkdown, error) {
	doc := books.ParseStructuredMarkdown(raw, c.contract.ID)
	return doc, contractFail(c.contract.ID, outlineViolations(doc, c.kind))
}

type planBookPayload struct {
	Cast                []map[string]any `json:"cast"`
	BookOutlineMarkdown string           `json:"bookOutlineMarkdown"`
	Settings            *struct {
		Files map[string]string `json:"files"`
	} `json:"settings"`
}

type planBookCodec struct{ markdownCodec }

func (c planBookCodec) Decode(raw string) (planBookPayload, error) {
	var out planBookPayload
	if err := unmarshalJSON(raw, &out); err != nil {
		return out, err
	}
	if _, err := c.markdownCodec.Decode(out.BookOutlineMarkdown); err != nil {
		return out, err
	}
	return out, nil
}

type planVolumePayload struct {
	VolumeOutlineMarkdown string           `json:"volumeOutlineMarkdown"`
	BookOutlineMarkdown   string           `json:"bookOutlineMarkdown"`
	Cast                  []map[string]any `json:"cast"`
	Settings              *struct {
		Files map[string]string `json:"files"`
	} `json:"settings"`
}
type planVolumeCodec struct{ markdownCodec }

func (c planVolumeCodec) Decode(raw string) (planVolumePayload, error) {
	var out planVolumePayload
	if err := unmarshalJSON(raw, &out); err != nil {
		return out, err
	}
	if _, err := c.markdownCodec.Decode(out.VolumeOutlineMarkdown); err != nil {
		return out, err
	}
	return out, nil
}

func ParseStructureEditor(raw string) ([]domain.ReviewFinding, error) {
	doc := books.ParseStructuredMarkdown(raw, books.StructureEditorProtocol)
	if doc.Legacy {
		return nil, fmt.Errorf("结构审校缺少 schema_version")
	}
	if len(doc.Diagnostics) > 0 {
		return nil, fmt.Errorf("结构审校协议错误：%s", books.FormatOutlineDiagnostics(doc.Diagnostics))
	}
	var out []domain.ReviewFinding
	for _, e := range doc.EntitiesOf("finding") {
		f := domain.ReviewFinding{ID: e.ID, Severity: e.Value("severity"), Lens: e.Value("lens"), Title: e.Value("entity_path"), EntityPath: e.Value("entity_path"), Evidence: e.Value("evidence"), Suggestion: e.Value("suggestion"), Source: "ai_review"}
		if f.ID == "" || (f.Severity != "blocking" && f.Severity != "advisory") || f.Title == "" || f.Evidence == "" || f.Suggestion == "" {
			return nil, fmt.Errorf("结构审校问题字段不完整: %s", e.ID)
		}
		out = append(out, f)
	}
	return out, nil
}

func decodeFullMarkdown(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) >= 3 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			raw = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	if !strings.HasPrefix(raw, "#") {
		return "", fmt.Errorf("输出必须是一个完整 Markdown 文档")
	}
	return raw, nil
}
