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
type UserPayload struct {
	Task        string         `json:"task"`
	Mode        string         `json:"mode,omitempty"`
	Book        map[string]any `json:"book"`
	Instruction string         `json:"instruction,omitempty"`
	Documents   []UserDoc      `json:"documents,omitempty"`
	References  []UserRef      `json:"references,omitempty"`
	Repair      map[string]any `json:"repair,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
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
