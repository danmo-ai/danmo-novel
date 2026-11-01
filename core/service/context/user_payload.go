package contextsvc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
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
// 字段顺序即序列化顺序，而 provider 的前缀缓存按字节比对 prompt 前缀。
// Repair 必须留在最后：重试时前面全部字节不变，只有尾部这块是新的，
// 于是前缀缓存能命中几乎整份上下文（正文/文档动辄数万 token）。把它插在
// 中间（例如 Extra 之前）会让它后面的一切整体后移，缓存全部失效。
type UserPayload struct {
	Task        string         `json:"task"`
	Mode        string         `json:"mode,omitempty"`
	Book        map[string]any `json:"book"`
	Instruction string         `json:"instruction,omitempty"`
	Documents   []UserDoc      `json:"documents,omitempty"`
	References  []UserRef      `json:"references,omitempty"`
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
		var b strings.Builder
		b.WriteString(userPrompt)
		for k, v := range extra {
			fmt.Fprintf(&b, "\n\n%s: %v", k, v)
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

// NarrowBeats returns a copy of a write packet whose 场面节拍 block only holds
// one chapter's scenes. Everything else (contract, facts, cast, style) is
// identical, so it is not rebuilt.
//
// 分章写作时这是最大的单项浪费：6 章单元每次调用都带全部 26 个场面，
// 而模型这一次只需要写其中一章。过滤不放宽任何约束——合同、facts、人物照旧。
func NarrowBeats(packet domain.ContextPacket, unit domain.UnitInfo, chapter int) domain.ContextPacket {
	out := packet
	out.Blocks = make([]domain.ContextBlock, 0, len(packet.Blocks))
	changed := false
	for _, b := range packet.Blocks {
		if b.ID == "unit-beats" {
			if trimmed := BeatsForChapter(unit.OutlineRaw, chapter); trimmed != "" && trimmed != b.Content {
				b.Content = trimmed
				b.Bytes = len(trimmed)
				changed = true
			}
		}
		out.Blocks = append(out.Blocks, b)
	}
	if !changed {
		return packet
	}
	// 用户 payload 是从 blocks 序列化出来的，必须同步重建，否则过滤不生效。
	out.UserPrompt = buildUserPromptFrom(packet, out.Blocks)
	out.EstPromptTokens = (len([]rune(out.SystemPrompt)) + len([]rune(out.UserPrompt))) / 2
	return out
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
