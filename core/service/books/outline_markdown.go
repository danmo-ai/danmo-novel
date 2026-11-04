package books

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

const (
	BookOutlineProtocol     = "book-outline/v1"
	VolumeOutlineProtocol   = "volume-outline/v1"
	StructureEditorProtocol = "structure-editor/v1"
)

var (
	entityHeadingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	entityNameRe    = regexp.MustCompile(`^(全书总纲|卷纲|故事线|分卷|单元|节拍|转折|承诺|约束|揭示|伏笔|卷末交接|问题|身份变更)\s*(?:·|:|：)?\s*(.*)$`)
	markdownFieldRe = regexp.MustCompile(`^\s*-\s+([a-z][a-z0-9_]*)\s*:\s*(.*)$`)
)

// 结构化 Markdown 的四个协议各自绑定一个文件。
const (
	// ForeshadowSchedulePath 是伏笔的唯一权威登记表。
	//
	// 它与 canon/reveal-schedule.md 的关系是**继承**而不是并列：揭示计划回答
	// 「读者此刻能不能知道某个秘密」，本质是 type=信息 的伏笔的一种。实测三本书
	// 83 条揭示里 81 条的 hint_unit_ids ≠ confirm_unit_id——它早就在做「A 单元埋、
	// B 单元收」，与伏笔是同一件事的两份登记。融合后一个事实只存在一处。
	ForeshadowSchedulePath = "canon/foreshadow-schedule.md"
	// ForeshadowProtocol 是伏笔登记表的结构化协议名。
	ForeshadowProtocol = "foreshadow-schedule/v1"
)

// ProtocolForPath 返回某个 Markdown 文件使用的结构化协议；无协议时返回空串。
//
// 以前这段判断在 loadBook 与 StructuredOutlineFor 里各写一遍，两边不一致时
// 同一份文件会按不同协议解析。现在只此一处。
func ProtocolForPath(path string) string {
	switch {
	case path == "outline/book_outline.md":
		return BookOutlineProtocol
	case strings.HasPrefix(path, "outline/volumes/") && strings.HasSuffix(path, ".md"):
		return VolumeOutlineProtocol
	case path == ForeshadowSchedulePath:
		return ForeshadowProtocol
	default:
		return ""
	}
}

// ParseStructuredMarkdown parses the deliberately small, human-editable
// Markdown grammar. Unknown structured syntax is diagnosed rather than
// silently dropped. Files without the version marker remain readable legacy.
func ParseStructuredMarkdown(raw, protocol string) domain.StructuredMarkdown {
	doc := domain.StructuredMarkdown{Protocol: protocol, Version: 1, Root: domain.MarkdownEntity{Kind: "root"}}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	markers := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "- schema_version:") {
			markers++
		}
	}
	if markers == 0 {
		doc.Legacy = true
		return doc
	}

	current := &doc.Root
	offset := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if h := entityHeadingRe.FindStringSubmatch(line); h != nil {
			level, heading := len(h[1]), h[2]
			if level == 1 {
				doc.Title = heading
				current = &doc.Root
				offset += len(line) + 1
				continue
			}
			if strings.HasPrefix(heading, "结构审校") {
				current = &doc.Root
				offset += len(line) + 1
				continue
			}
			name := entityNameRe.FindStringSubmatch(heading)
			if name == nil {
				// Notes and free-form explanations are explicitly non-contract.
				current = &domain.MarkdownEntity{Kind: "note", ID: heading}
				doc.Entities = append(doc.Entities, *current)
				current = &doc.Entities[len(doc.Entities)-1]
				offset += len(line) + 1
				continue
			}
			kind, id := name[1], strings.TrimSpace(name[2])
			if kind == "全书总纲" || kind == "卷纲" {
				doc.Title = heading
				current = &doc.Root
				offset += len(line) + 1
				continue
			}
			if kind == "问题" {
				kind = "finding"
			}
			entity := domain.MarkdownEntity{Kind: kind, ID: id, Range: domain.SourceRange{Line: i + 1, Column: 1, ByteOffset: offset}}
			doc.Entities = append(doc.Entities, entity)
			current = &doc.Entities[len(doc.Entities)-1]
			if id == "" {
				doc.Diagnostics = append(doc.Diagnostics, diagnostic(kind, "实体标题缺少稳定 ID", i+1))
			}
			offset += len(line) + 1
			continue
		}
		if f := markdownFieldRe.FindStringSubmatch(line); f != nil {
			value := strings.TrimRight(f[2], " \t")
			start := strings.Index(line, f[2])
			fieldStart := offset + start
			endLine := i + 1
			end := offset + len(line)
			for j := i + 1; j < len(lines); j++ {
				next := lines[j]
				if strings.TrimSpace(next) == "" {
					// A blank line terminates a Markdown field. Never consume
					// indentation after it, or the next entity would be swallowed.
					break
				}
				if (strings.HasPrefix(next, "  ") || strings.HasPrefix(next, "\t")) && !strings.HasPrefix(strings.TrimSpace(next), "-") && !entityHeadingRe.MatchString(next) {
					value += "\n" + strings.TrimPrefix(strings.TrimPrefix(next, "  "), "\t")
					offset += len(line) + 1
					line = next
					i = j
					endLine = i + 1
					end = offset + len(next)
					continue
				}
				break
			}
			if current.Kind == "note" {
				offset += len(line) + 1
				continue
			}
			field := domain.MarkdownField{Name: f[1], Value: value, Range: domain.SourceRange{
				Line: endLine, Column: start + 1, EndLine: endLine, EndColumn: len(line) + 1,
				ByteOffset: fieldStart, ByteEnd: end,
			}}
			// Point at the beginning of the field, not the last continuation.
			field.Range.Line = endLine - strings.Count(value, "\n")
			for _, old := range current.Fields {
				if old.Name == field.Name {
					doc.Diagnostics = append(doc.Diagnostics, diagnostic(current.ID+"."+field.Name, "字段重复", field.Range.Line))
				}
			}
			current.Fields = append(current.Fields, field)
		} else if strings.HasPrefix(trimmed, "-") && current.Kind != "note" && trimmed != "-" {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic(current.ID, "无法识别的字段；使用 - field_name: 内容", i+1))
		}
		offset += len(line) + 1
	}

	seen := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Kind == "note" {
			continue
		}
		key := e.Kind + "/" + e.ID
		if seen[key] {
			doc.Diagnostics = append(doc.Diagnostics, diagnostic(key, "实体 ID 重复", e.Range.Line))
		}
		seen[key] = true
	}
	if doc.Root.Value("schema_version") != "1" {
		doc.Diagnostics = append(doc.Diagnostics, diagnostic("schema_version", "当前仅支持版本 1", 1))
	}
	return doc
}

func diagnostic(path, message string, line int) domain.OutlineDiagnostic {
	return domain.OutlineDiagnostic{Path: path, Message: message, Severity: "blocking", Line: line}
}

// ReplaceMarkdownField applies a minimal source patch. It preserves every
// other byte of the author's Markdown, including comments and ordering.
func ReplaceMarkdownField(raw, protocol, kind, id, field, value string) (string, error) {
	doc := ParseStructuredMarkdown(raw, protocol)
	if doc.Legacy {
		return "", fmt.Errorf("旧格式 Markdown 请先通过 Agent 批量迁移，不能局部结构化保存")
	}
	if len(doc.Diagnostics) > 0 {
		return "", fmt.Errorf("原文存在结构错误，请先修复：%s", doc.Diagnostics[0].Message)
	}
	entity := doc.Root
	if kind != "root" {
		var ok bool
		entity, ok = doc.Entity(kind, id)
		if !ok {
			return "", fmt.Errorf("实体不存在: %s/%s", kind, id)
		}
	}
	f, ok := entity.Field(field)
	if !ok {
		return "", fmt.Errorf("字段不存在: %s.%s", entity.ID, field)
	}
	if strings.ContainsAny(value, "\r") {
		return "", fmt.Errorf("字段包含非法换行")
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("字段值不能为空")
	}
	if f.Range.ByteOffset < 0 || f.Range.ByteEnd < f.Range.ByteOffset || f.Range.ByteEnd > len(raw) {
		return "", fmt.Errorf("字段源位置无效，已阻止覆盖原文件")
	}
	// Continuation lines are indented so they remain part of this field.
	value = strings.ReplaceAll(value, "\n", "\n  ")
	return raw[:f.Range.ByteOffset] + value + raw[f.Range.ByteEnd:], nil
}

func FormatOutlineDiagnostics(ds []domain.OutlineDiagnostic) string {
	var b strings.Builder
	for i, d := range ds {
		if i > 0 {
			b.WriteByte('\n')
		}
		if d.Line > 0 {
			fmt.Fprintf(&b, "第 %d 行 ", d.Line)
		}
		fmt.Fprintf(&b, "%s：%s", d.Path, d.Message)
	}
	return b.String()
}

func StructuredOutlineFor(snap domain.BookSnapshot, path string) (domain.StructuredMarkdown, bool) {
	if doc, ok := snap.StructuredOutlines[path]; ok && doc != nil {
		return *doc, true
	}
	raw, ok := snap.Files[path]
	if !ok {
		return domain.StructuredMarkdown{}, false
	}
	protocol := ProtocolForPath(path)
	if protocol == "" {
		protocol = VolumeOutlineProtocol
	}
	return ParseStructuredMarkdown(raw, protocol), true
}

// stateDeltasFromMarkdown converts the compact authoring syntax
// "stem.field=before->after; stem.goal=old->new" to the existing ledger
// contract. It does not introduce a second continuity representation.
func stateDeltasFromMarkdown(raw string) []map[string]string {
	out := []map[string]string{}
	for _, item := range splitList(raw) {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		target := strings.SplitN(strings.TrimSpace(parts[0]), ".", 2)
		transition := strings.SplitN(parts[1], "->", 2)
		if len(target) != 2 || len(transition) != 2 {
			continue
		}
		stem, field := strings.TrimSpace(target[0]), strings.TrimSpace(target[1])
		if stem == "" || field == "" {
			continue
		}
		out = append(out, map[string]string{"stem": stem, "field": field, "from": strings.TrimSpace(transition[0]), "to": strings.TrimSpace(transition[1])})
	}
	return out
}

// StateDeltaKeysFromMarkdown exposes the canonical parent comparison keys.
func StateDeltaKeysFromMarkdown(raw string) []string {
	var out []string
	for _, row := range stateDeltasFromMarkdown(raw) {
		out = append(out, row["stem"]+"."+row["field"]+"="+row["from"]+"->"+row["to"])
	}
	return out
}

// splitList splits a comma/semicolon separated author list.
//
// 作者和模型常用行内数组写法（"[career,relationship]"）。以前只切逗号，
// 于是一份合法的卷纲会生成 thread_ids: ["[career", "relationship]"] —— 下游
// 按 thread_id 找故事线永远对不上。方括号属于容器语法，不是值的一部分。
func splitList(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "【"), "】"))
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == '；' })
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(p, "[]【】"))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func ChapterWindow(value string) (int, int) {
	parts := strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune("-–—~至到", r) })
	if len(parts) != 2 {
		return 0, 0
	}
	from, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	to, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return from, to
}
