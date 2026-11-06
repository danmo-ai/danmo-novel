package contextsvc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
)

// UserDoc is one document attached as data (never mixed into the system template).
type UserDoc struct {
	ID      string `json:"id"`
	Path    string `json:"path,omitempty"`
	Label   string `json:"label,omitempty"`
	Content string `json:"content"`
}

// UserRef is optional author-attached reference knowledge.
type UserRef struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
}

// UserPayload is the only content of the user message for controlled LLM calls.
//
// **字段顺序即序列化顺序，而 provider 的前缀缓存按字节比对 prompt 前缀。**
// 这决定了整个结构必须满足两条硬约束：
//
//  1. 所有「这一章都一样」的东西排在前面：mode/book/documents/references。
//     审校与修订拿到的是同一份正文与同一批上下文块，这一段逐字相同。
//  2. 所有「每任务/每次尝试才不同」的东西排在后面：task/instruction/extra/repair。
//     它们一旦插在中间，后面的一切整体后移，缓存全部失效。
//
// 曾经 task 与 instruction 排在最前面（第 3、4 个字段），而它们恰恰是审校
// （"review"）与修订（"revise_patch"）第一个就不同的地方——于是 13819 字的
// 共享正文一个 token 都没命中。Repair 同理必须留在最后（见 MergeRepairIntoUserPayload）。
//
// 分章写作同样受这条约束支配：documents 里出现任何按章变化的内容（最典型的是
// 场面节拍），公共前缀就会停在 documents 中间，后面的上下文块与 references
// 全部按全价重算。所以按章切片的东西一律走 extra（见 ChapterBeatsKey），
// **不要往 documents 里替换内容**。
type UserPayload struct {
	Mode        string         `json:"mode,omitempty"`
	Book        map[string]any `json:"book"`
	Documents   []UserDoc      `json:"documents,omitempty"`
	References  []UserRef      `json:"references,omitempty"`
	Task        string         `json:"task"`
	Instruction string         `json:"instruction,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
	Repair      map[string]any `json:"repair,omitempty"`
}

// EncodeUserPayload marshals the structured user data packet.
func EncodeUserPayload(p UserPayload) string {
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Sprintf(`{"task":%q,"error":"encode failed"}`, p.Task)
	}
	return string(b)
}

// DecodeUserPayload parses a user JSON payload.
func DecodeUserPayload(raw string) (UserPayload, error) {
	var p UserPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &p); err != nil {
		return UserPayload{}, err
	}
	return p, nil
}

// MergeKnowledgeIntoUserPayload appends reference docs into the JSON user payload.
func MergeKnowledgeIntoUserPayload(userPrompt string, docs []string) string {
	if len(docs) == 0 {
		return userPrompt
	}
	p, err := DecodeUserPayload(userPrompt)
	if err != nil {
		// Fallback for legacy prose prompts during migration of other tasks.
		var b strings.Builder
		b.WriteString(userPrompt)
		b.WriteString("\n\n## 参考知识（作者附加，仅作方法/风格参考，不写入正文事实）\n")
		for _, doc := range docs {
			b.WriteString("\n")
			b.WriteString(doc)
		}
		return b.String()
	}
	for i, doc := range docs {
		p.References = append(p.References, UserRef{
			ID:      fmt.Sprintf("knowledge-%d", i+1),
			Title:   "参考知识",
			Content: doc,
		})
	}
	return EncodeUserPayload(p)
}

// MergeRepairIntoUserPayload attaches directed repair evidence into the JSON payload.
func MergeRepairIntoUserPayload(userPrompt string, err error) string {
	if err == nil {
		return userPrompt
	}
	p, decErr := DecodeUserPayload(userPrompt)
	if decErr != nil {
		return userPrompt + "\n\n上一次输出未通过校验，请定向修复：\n" + err.Error()
	}
	p.Repair = map[string]any{
		"message": err.Error(),
		"rule":    "保留已通过部分原样，只修复指出的问题，返回完整合规 JSON。",
	}
	return EncodeUserPayload(p)
}

// RetaskUserPayload rewrites the payload's task tag when one packet is reused for
// a different task (审校包 → 定向修订). 系统提示词与输出契约仍由调用方一起替换，
// 这里只保证模型看到的 task 标签不会与实际任务相互矛盾。
func RetaskUserPayload(userPrompt, task string) string {
	p, err := DecodeUserPayload(userPrompt)
	if err != nil {
		return userPrompt
	}
	p.Task = task
	return EncodeUserPayload(p)
}

// MergeExtraIntoUserPayload merges key/values into the payload Extra map.
func MergeExtraIntoUserPayload(userPrompt string, extra map[string]any) string {
	if len(extra) == 0 {
		return userPrompt
	}
	p, err := DecodeUserPayload(userPrompt)
	if err != nil {
		// 老格式（非 JSON）payload 的兜底拼装。这里是唯一把 map 直接遍历成
		// 自然语言的地方：不排序的话，同一组 extra 每次运行都会得到不同字节序，
		// 前缀缓存与"同输入同输出"同时失效。
		var b strings.Builder
		b.WriteString(userPrompt)
		for _, k := range sortedMapKeys(extra) {
			fmt.Fprintf(&b, "\n\n%s: %v", k, extra[k])
		}
		return b.String()
	}
	if p.Extra == nil {
		p.Extra = map[string]any{}
	}
	for k, v := range extra {
		p.Extra[k] = v
	}
	return EncodeUserPayload(p)
}

// DocumentsFromBlocks converts context blocks into user documents.
func DocumentsFromBlocks(blocks []domain.ContextBlock) []UserDoc {
	out := make([]UserDoc, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, UserDoc{
			ID:      b.ID,
			Path:    b.Source,
			Label:   b.Label,
			Content: b.Content,
		})
	}
	return out
}

// BuildUserPrompt encodes the standard two-message user data packet.
func BuildUserPrompt(task, mode string, book map[string]any, instruction string, blocks []domain.ContextBlock, extra map[string]any) string {
	return EncodeUserPayload(UserPayload{
		Task:        task,
		Mode:        mode,
		Book:        book,
		Instruction: instruction,
		Documents:   DocumentsFromBlocks(blocks),
		Extra:       extra,
	})
}

// ChapterBeatsKey 是按章切片后的场面节拍在 user payload 尾部的键名。
//
// 它必须落在 extra（序列化在 references 之后），不能留在 documents 里：
// documents 排在 references 之前，而 references 是作者勾选的参考知识
// （实测单本 32KB，占写作 prompt 的三分之二）。节拍是唯一按章变化的文档，
// 把它留在 documents 里，公共前缀就只能停在 documents 开头——实测单章命中
// 率 5%，等于每章把整份上下文按全价重算一遍。
const ChapterBeatsKey = "chapterBeats"

// WriteChapterExtra 拼装「只对这一章成立」的 payload 尾部字段。
//
// 抽出来是为了让真实调用与预览走同一份代码：这些字段按章变化，一旦中途分叉，
// documents 之后的全部内容（人物卡、连续性、风格、整份参考知识）都无法命中
// 前缀缓存；而两处各拼一份时，还会出现「预览漏了上一章尾部」这种看起来像
// 数据缺失的假象——实测就是这么被误判的。
//
// generated 是本次已经写出的章节（按序）。第 N 章取它里面的第 N-1 章；
// 取不到时回退到本单元正文、再回退到上一单元正文。
func WriteChapterExtra(
	snap domain.BookSnapshot, unit domain.UnitInfo, ch, target, floor, ceil int,
	beats string, generated []domain.ChapterDraft,
) map[string]any {
	extra := map[string]any{
		"chapter": ch,
		"chapterRule": fmt.Sprintf(
			"本次只写第 %d 章，chapters 数组只放这一章。本章为 %d 字（允许 %d–%d 字）：先按场面分配字数，写完收在目标附近，不要靠追加场景或复述把篇幅写长。",
			ch, target, floor, ceil),
	}
	if beats != "" {
		extra[ChapterBeatsKey] = beats
	}
	// 本章伏笔：只给本章要埋/要收的条目与已埋未收清单。
	if foreshadow := ForeshadowForChapter(snap, unit, ch); foreshadow != "" {
		extra[ChapterForeshadowsKey] = foreshadow
	}
	if tail, source := PreviousChapterTail(snap, unit, generated, ch); tail != "" {
		extra["previousChapterTail"] = map[string]any{
			"chapter": ch - 1, "body": tail, "source": source,
			"note": "仅供语感与承接，不要复述上一章剧情",
		}
	}
	// 上一章「已接受事实账本」的桥接：只在单元边界上存在。本单元内部上一章
	// 刚由本次 run 写出、尚未定稿，不构成账本，此时正文尾部就是唯一合法承接。
	// 见 chapter_ledger.go 的分工说明。
	if ledger := PreviousChapterLedger(snap, unit, ch); ledger != "" {
		extra["previousChapterLedger"] = ledger
	}
	return extra
}

// WritePreviewPacket 返回「写正文真正会发出去的第一章包」。
//
// 以前预览直接返回 BuildWritePacket，与真实请求差了三项按章字段：chapter、
// chapterRule、previousChapterTail。文档与界面都写着「预览就是模型会收到的
// 那一份」，于是作者用预览核对衔接时会得出"上一章尾部根本没带上"的结论——
// 实际上带上了，只是预览没显示。预览的价值全在可信，所以补齐。
//
// 只展示第一章：run 是逐章串行推进的，第一章的包是唯一在「参数相同即可复现」
// 意义上确定的那一份；后续章的 previousChapterTail 取决于模型刚写出来的内容。
func WritePreviewPacket(snap domain.BookSnapshot, unit domain.UnitInfo) domain.ContextPacket {
	return WritePreviewPacketFor(snap, unit, "", "")
}

// WritePreviewPacketFor 是 WritePreviewPacket 的完整签名版本（带作者指令与重写模式）。
func WritePreviewPacketFor(snap domain.BookSnapshot, unit domain.UnitInfo, instruction, mode string) domain.ContextPacket {
	packet := BuildWritePacketFor(snap, unit, instruction, mode)
	from := unit.ChapterFrom
	if from <= 0 {
		from = 1
	}
	chapterPacket, beats := NarrowBeats(packet, unit, from)
	target := books.ChapterTarget(unit)
	floor, ceil := domain.ChapterBand(target)
	extra := WriteChapterExtra(snap, unit, from, target, floor, ceil, beats, nil)
	chapterPacket.UserPrompt = MergeExtraIntoUserPayload(chapterPacket.UserPrompt, extra)
	chapterPacket.EstPromptTokens = (len([]rune(chapterPacket.SystemPrompt)) + len([]rune(chapterPacket.UserPrompt))) / 2
	return chapterPacket
}

// PreviousChapterTail 返回本章前一章的文末，用于语言级衔接。
//
// 它替代原来两套并存的做法：documents 里的 prev-tail（上**单元**文末，每章都带）
// 与 extra 里塞的上一章**全文**。前者只对单元第一章有意义，后者实测每章多付
// 2,500–7,000 字——而衔接只需要文末的语感与钩子，不需要重读整章剧情。
//
// 查找顺序：本次已生成的章节 → 本单元正文 → 上一单元正文。
// 最后一步是必需的：单元第一章的前一章住在上一单元里，漏掉它整条衔接就断了。
func PreviousChapterTail(snap domain.BookSnapshot, unit domain.UnitInfo, generated []domain.ChapterDraft, chapter int) (string, string) {
	for i := len(generated) - 1; i >= 0; i-- {
		if generated[i].Chapter == chapter-1 {
			return clipTail(generated[i].Body, 800), unit.ProsePath
		}
	}
	for _, ch := range gates.ParseProseChapters(unit.ProseRaw) {
		if ch.Chapter == chapter-1 {
			return clipTail(ch.Body, 800), unit.ProsePath
		}
	}
	prev := previousUnit(snap, unit)
	if prev == nil {
		return "", ""
	}
	for _, ch := range gates.ParseProseChapters(prev.ProseRaw) {
		if ch.Chapter == chapter-1 {
			return clipTail(ch.Body, 800), prev.ProsePath
		}
	}
	// 章号不可解析时退化成「上一单元文末」：有语感可承接，胜过什么都没有。
	return clipTail(prev.ProseRaw, 800), prev.ProsePath
}

// NarrowBeats 把整单元的场面包裁剪成本章的一小段，并从 documents 中摘掉，
// 交给调用方放进 payload 尾部（extra[ChapterBeatsKey]）。
//
// 返回值：去掉 unit-beats 之后的 packet 副本，以及本章的场面节拍文本
// （原文没有节拍块时为空串，调用方此时不需要补 extra）。
//
// 为什么是「整块移出」而不是「原地替换内容」：替换仍然改变 documents 里
// 那一段字节，排在它后面的每个块（人物卡、连续性、风格、揭示约束）与整份
// references 都跟着失去复用。移出之后，documents / references / task /
// instruction 逐字相同，分叉点只剩尾部 extra 里的章号、节拍与上一章正文。
//
// 过滤本身不放宽任何约束：合同、facts、人物照旧，只有节拍按章切片。
func NarrowBeats(packet domain.ContextPacket, unit domain.UnitInfo, chapter int) (domain.ContextPacket, string) {
	beats := BeatsForChapter(unit.OutlineRaw, chapter)
	if strings.TrimSpace(beats) == "" {
		return packet, ""
	}
	out := packet
	out.Blocks = make([]domain.ContextBlock, 0, len(packet.Blocks))
	removed := false
	for _, b := range packet.Blocks {
		if b.ID == "unit-beats" {
			removed = true
			continue
		}
		out.Blocks = append(out.Blocks, b)
	}
	if !removed {
		return packet, ""
	}
	// 用户 payload 是从 blocks 序列化出来的，必须同步重建，否则摘除不生效。
	out.UserPrompt = buildUserPromptFrom(packet, out.Blocks)
	out.EstPromptTokens = (len([]rune(out.SystemPrompt)) + len([]rune(out.UserPrompt)) + len([]rune(beats))) / 2
	return out, beats
}

// buildUserPromptFrom re-encodes the user payload with replacement blocks while
// preserving the original task/mode/book/instruction/extra fields.
func buildUserPromptFrom(packet domain.ContextPacket, blocks []domain.ContextBlock) string {
	p, err := DecodeUserPayload(packet.UserPrompt)
	if err != nil {
		return packet.UserPrompt
	}
	p.Documents = DocumentsFromBlocks(blocks)
	return EncodeUserPayload(p)
}

// sortedMapKeys 把 map 的键排成稳定顺序。
//
// 凡是「把 map 遍历成提示词文本」的地方都必须走它：Go 的 map 迭代顺序是随机的，
// 同一份输入两次构建会得到不同字节序，前缀缓存和「同输入同输出」一起失效。
// 注意 json.Marshal 对 map 已经按键排序，只有手写拼接的路径需要这个。
func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
