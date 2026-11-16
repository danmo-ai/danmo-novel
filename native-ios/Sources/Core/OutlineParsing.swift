import Foundation

/// Presentation-only fallback parser for candidate outlines, mirroring
/// `frontend/src/shared/utils/structuredMarkdown.ts`. The Go parser is the
/// sole source of truth for validation, saving and LLM context; this never
/// persists data.
enum StructuredMarkdownParser {
    static func parse(_ raw: String, protocolName: String) -> StructuredMarkdown {
        var entities: [MarkdownEntity] = []
        var rootFields: [MarkdownField] = []
        var title = ""
        var current: Int? // index into entities; nil = root
        let lines = raw.replacingOccurrences(of: "\r\n", with: "\n").split(separator: "\n", omittingEmptySubsequences: false)
        for line in lines {
            if let heading = Rx.whole("(#{1,6})\\s+(.+?)", in: String(line)) {
                let text = heading[2].trimmingCharacters(in: .whitespaces)
                if heading[1].count == 1 || text.hasPrefix("全书总纲") || text.hasPrefix("卷纲") || text.hasPrefix("结构审校") {
                    title = text
                    current = nil
                    continue
                }
                guard let match = Rx.whole("(故事线|分卷|单元|节拍|转折|承诺|约束|揭示|卷末交接|问题)\\s*(?:·|:|：)?\\s*(.*)", in: text) else { continue }
                let kind = match[1] == "问题" ? "finding" : match[1]
                entities.append(MarkdownEntity(kind: kind, id: match[2].trimmingCharacters(in: .whitespaces), fields: []))
                current = entities.count - 1
                continue
            }
            if let field = Rx.whole("\\s*-\\s+([a-z][a-z0-9_]*)\\s*:\\s*(.*)", in: String(line)) {
                let parsed = MarkdownField(name: field[1], value: field[2].trimmingCharacters(in: .whitespaces))
                if let current {
                    entities[current] = MarkdownEntity(kind: entities[current].kind, id: entities[current].id, fields: entities[current].fields + [parsed])
                } else {
                    rootFields.append(parsed)
                }
            }
        }
        return StructuredMarkdown(
            protocolName: protocolName,
            version: 1,
            title: title,
            root: MarkdownEntity(kind: "root", id: "", fields: rootFields),
            entities: entities,
            diagnostics: nil,
            legacy: !raw.contains("- schema_version:")
        )
    }
}

/// Structured rendering of plain Markdown docs (bible / knowledge), mirroring
/// the desktop's block parser: headings, paragraphs, bullets, quotes, tables.
enum MarkdownBlocks {
    enum Block: Hashable, Identifiable {
        case heading(level: Int, text: String)
        case paragraph(String)
        case bullets([String])
        case quote(String)
        case table(headers: [String], rows: [[String]])

        var id: String {
            switch self {
            case .heading(let level, let text): "h\(level)-\(text)"
            case .paragraph(let text): "p-\(text)"
            case .bullets(let items): "ul-\(items.joined())"
            case .quote(let text): "q-\(text)"
            case .table(let headers, _): "t-\(headers.joined())"
            }
        }
    }

    static func cleanInline(_ text: String) -> String {
        var result = text
        if let regex = try? NSRegularExpression(pattern: "\\*\\*([^*]+)\\*\\*") {
            result = regex.stringByReplacingMatches(in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1")
        }
        if let regex = try? NSRegularExpression(pattern: "`([^`]+)`") {
            result = regex.stringByReplacingMatches(in: result, range: NSRange(result.startIndex..., in: result), withTemplate: "$1")
        }
        return result.trimmingCharacters(in: .whitespaces)
    }

    static func parse(_ raw: String) -> [Block] {
        var blocks: [Block] = []
        var bullets: [String] = []
        var paragraph: [String] = []
        func flushParagraph() {
            if !paragraph.isEmpty { blocks.append(.paragraph(paragraph.joined(separator: " "))); paragraph = [] }
        }
        func flushBullets() {
            if !bullets.isEmpty { blocks.append(.bullets(bullets)); bullets = [] }
        }
        let lines = raw.replacingOccurrences(of: "\r\n", with: "\n").split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        var index = 0
        while index < lines.count {
            let trimmed = lines[index].trimmingCharacters(in: .whitespaces)
            if let heading = Rx.whole("(#{1,4})\\s+(.*)", in: trimmed) {
                flushParagraph(); flushBullets()
                blocks.append(.heading(level: heading[1].count, text: cleanInline(heading[2])))
                index += 1
                continue
            }
            if trimmed.hasPrefix("|") {
                flushParagraph(); flushBullets()
                var tableLines: [String] = []
                while index < lines.count, lines[index].trimmingCharacters(in: .whitespaces).hasPrefix("|") {
                    tableLines.append(lines[index].trimmingCharacters(in: .whitespaces))
                    index += 1
                }
                let rows = tableLines
                    .filter { !Rx.matches("^\\|?\\s*:?-{2,}", in: $0, multiline: false) }
                    .map { line -> [String] in
                        let body = line.trimmingCharacters(in: CharacterSet(charactersIn: "|"))
                        return body.split(separator: "|", omittingEmptySubsequences: false).map { cleanInline(String($0)) }
                    }
                if rows.count > 1 { blocks.append(.table(headers: rows[0], rows: Array(rows.dropFirst()))) }
                continue
            }
            if let bullet = Rx.whole("[-*]\\s+(.*)", in: trimmed) {
                flushParagraph()
                bullets.append(cleanInline(bullet[1]))
                index += 1
                continue
            }
            if trimmed.hasPrefix(">") {
                flushParagraph(); flushBullets()
                blocks.append(.quote(cleanInline(String(trimmed.dropFirst().trimmingCharacters(in: .whitespaces)))))
                index += 1
                continue
            }
            if trimmed.isEmpty {
                flushParagraph(); flushBullets()
                index += 1
                continue
            }
            paragraph.append(cleanInline(trimmed))
            index += 1
        }
        flushParagraph(); flushBullets()
        return blocks
    }
}

/// novel-state.yaml → labeled fields + gate chips (desktop parseState).
struct StateView {
    struct Field: Hashable, Identifiable { let key: String; let label: String; let value: String; var id: String { key } }
    struct Gate: Hashable, Identifiable { let key: String; let value: String; var id: String { key } }

    let fields: [Field]
    let gates: [Gate]

    static func parse(_ raw: String) -> StateView {
        var fields: [Field] = []
        var gates: [Gate] = []
        var inGates = false
        for line in raw.split(separator: "\n", omittingEmptySubsequences: false) {
            if let match = Rx.whole("([a-z_]+):\\s*(.*)", in: String(line)) {
                let key = match[1]
                inGates = key == "gates"
                if key == "gates" || key == "blockers" { continue }
                guard let label = Copy.stateFieldLabel(key) else { continue }
                var value = match[2].trimmingCharacters(in: CharacterSet(charactersIn: "'\" ")).trimmingCharacters(in: .whitespaces)
                if key == "stage" { value = Copy.stageText(value) }
                if key == "continuation_mode" { value = value == "true" ? "开" : "关" }
                fields.append(Field(key: key, label: label, value: value.isEmpty ? Copy.emptyValue : value))
                continue
            }
            if inGates, let gate = Rx.whole("\\s{2}([a-z_]+):\\s*(.+)", in: String(line)) {
                gates.append(Gate(key: gate[1], value: gate[2].trimmingCharacters(in: .whitespaces)))
            }
        }
        return StateView(fields: fields, gates: gates)
    }
}

/// canon/locked-terms.yaml → locked terms / compliance / aliases (desktop parseLockedTerms).
struct LockedTermsView {
    struct Term: Hashable, Identifiable { let term: String; let until: String; var id: String { term } }
    struct Alias: Hashable, Identifiable { let alias: String; let real: String; var id: String { alias } }

    let locked: [Term]
    let compliance: [String]
    let aliases: [Alias]

    static func parse(_ raw: String) -> LockedTermsView {
        var locked: [Term] = []
        var compliance: [String] = []
        var aliases: [Alias] = []
        var section = ""
        for line in raw.split(separator: "\n", omittingEmptySubsequences: false) {
            if line.hasPrefix("locked_until:") { section = "locked"; continue }
            if line.hasPrefix("compliance:") { section = "compliance"; continue }
            if line.hasPrefix("aliases:") { section = "aliases"; continue }
            switch section {
            case "locked":
                if let match = Rx.whole("\\s{2}([^:]+):\\s*(.+)", in: String(line)) {
                    locked.append(Term(term: MarkdownBlocks.cleanInline(match[1]), until: match[2].trimmingCharacters(in: .whitespaces)))
                }
            case "compliance":
                if let match = Rx.whole("\\s*-\\s*(.+)", in: String(line)) {
                    compliance.append(MarkdownBlocks.cleanInline(match[1]))
                }
            case "aliases":
                if let match = Rx.whole("\\s{2}([^:]+):\\s*(.+)", in: String(line)) {
                    aliases.append(Alias(alias: MarkdownBlocks.cleanInline(match[1]), real: match[2].trimmingCharacters(in: .whitespaces)))
                }
            default: break
            }
        }
        return LockedTermsView(locked: locked, compliance: compliance, aliases: aliases)
    }
}