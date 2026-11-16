import Foundation

// MARK: - Book

struct BookSummary: Decodable, Identifiable, Hashable {
    let id: String
    let title: String
    let genre: String
    let subgenre: String?
    let stage: String
    let lastCommittedCh: Int?
    let nextAction: String?
    let timeSystem: String?
    let unitCount: Int
    let draftedCount: Int?
    let reviewedCount: Int
    let openCandidates: Int
    let rootPath: String?
    let readOnly: Bool

    var progress: Double {
        unitCount == 0 ? 0 : Double(reviewedCount) / Double(unitCount)
    }
}

struct UnitInfo: Decodable, Identifiable, Hashable {
    let unitId: String
    let volume: Int
    let index: Int
    let status: String
    let title: String
    let chapterFrom: Int
    let chapterTo: Int
    let hasProse: Bool
    let outlinePath: String?
    let prosePath: String
    /// Derived per-chapter word target (books.ChapterTarget). Never parse
    /// word_target from the outline: that field is the unit total.
    let chapterTarget: Int?
    let outlineRaw: String?
    let proseRaw: String?

    var id: String { unitId }
    /// 正文已复核（reviewed）或账本已提交（finalized），都算完成态。
    var isFinished: Bool { status == "reviewed" || status == "finalized" }

    /// Vocabulary lives in `Copy` so iOS and Android cannot drift; the table is
    /// `docs/mobile-copy.md`. `reviewed` is finished but is not 已定稿.
    var statusTitle: String {
        switch status {
        case "finalized": Copy.statusFinalized
        case "reviewed": Copy.statusReviewed
        case "drafted": Copy.statusDrafted
        case "accepted": Copy.statusAccepted
        case "proposed": Copy.statusProposed
        default: status
        }
    }
}

/// A parsed structured-Markdown outline (book or volume). Mirrors the Go
/// parser's projection; presentation only, never persisted.
struct StructuredMarkdown: Decodable, Hashable {
    let protocolName: String
    let version: Int?
    let title: String
    let root: MarkdownEntity
    let entities: [MarkdownEntity]
    let diagnostics: [OutlineDiagnostic]?
    let legacy: Bool

    enum CodingKeys: String, CodingKey {
        case protocolName = "protocol"
        case version, title, root, entities, diagnostics, legacy
    }
}

struct MarkdownEntity: Decodable, Hashable, Identifiable {
    let kind: String
    let id: String
    let fields: [MarkdownField]

    func value(_ name: String) -> String {
        fields.first { $0.name == name }?.value ?? ""
    }
}

struct MarkdownField: Decodable, Hashable {
    let name: String
    let value: String
}

struct OutlineDiagnostic: Decodable, Hashable {
    let path: String
    let message: String
    let severity: String
    let line: Int?
}

struct VolumeInfo: Decodable, Hashable, Identifiable {
    let volume: Int
    let fileName: String
    let cast: [String]?
    let raw: String?
    let structured: StructuredMarkdown?

    var id: Int { volume }

    /// The volume's H1 title, for group headers.
    var title: String {
        raw?.split(separator: "\n").first { $0.hasPrefix("# ") }
            .map { String($0.dropFirst(2)).trimmingCharacters(in: .whitespaces) } ?? ""
    }
}

struct CastCard: Decodable, Hashable, Identifiable {
    let stem: String
    let name: String
    let tagline: String?
    let status: String
    let role: String
    var raw: String?

    var id: String { stem }
    var isCandidate: Bool { status == "candidate" }
    var roleTitle: String { Copy.castRole(role) }
}

struct IdentitySegment: Codable, Hashable {
    var id: String?
    var stem: String
    var roleTitle: String
    var org: String?
    var fromUnit: String
    var toUnit: String?
    var note: String?
}

/// On-disk book snapshot shape. The API returns it slim (skeleton, or skeleton
/// + panel files with `include=files`); fields are `var` so the workspace can
/// merge the lazy panel-document projection onto the skeleton.
struct BookSnapshot: Decodable, Hashable {
    var summary: BookSummary
    var stateRaw: String?
    var volumes: [VolumeInfo]
    var units: [UnitInfo]
    var cast: [CastCard]
    var files: [String: String]?
    var revision: String
    var identitySegments: [IdentitySegment]?
    var structuredOutlines: [String: StructuredMarkdown]?

    func file(_ path: String) -> String { files?[path] ?? "" }

    func unit(_ id: String) -> UnitInfo? { units.first { $0.unitId == id } }

    func castName(_ stem: String) -> String {
        cast.first { $0.stem == stem }?.name ?? stem
    }

    /// The identity segment in force at a unit (transitions only at unit
    /// boundaries; latest fromUnit at or before the unit wins).
    func identityAt(stem: String, unitID: String) -> String {
        let key = Self.unitKey(unitID)
        var best: IdentitySegment?
        for segment in identitySegments ?? [] where segment.stem == stem {
            let from = Self.unitKey(segment.fromUnit)
            if from > key { continue }
            if let to = segment.toUnit, Self.unitKey(to) < key { continue }
            if best == nil || from >= Self.unitKey(best!.fromUnit) { best = segment }
        }
        return best?.roleTitle ?? ""
    }

    static func unitKey(_ id: String) -> Int {
        guard let match = try? /v(\d+)-U(\d+)/.firstMatch(in: id),
              let volume = Int(match.1), let unit = Int(match.2) else { return 0 }
        return volume * 1000 + unit
    }
}

/// Lightweight navigation projection (the default book response).
struct BookDetail: Decodable, Hashable {
    let summary: BookSummary
    let units: [UnitInfo]
    let revision: String
}

struct PrimaryAction: Decodable, Hashable {
    let kind: String
    let unitId: String?
    let volume: Int?

    /// The Go handler sends "" for "no unit"; normalize to nil.
    var unit: String? {
        guard let unitId, !unitId.isEmpty else { return nil }
        return unitId
    }
}

struct ContinuitySummary: Decodable, Hashable {
    let units: Int
    let lastUnit: String?
    let afterCh: Int?
    let cast: Int
    let openLoops: Int
}

struct BookResponse: Decodable {
    let book: BookSnapshot
    let primary: PrimaryAction?
    let continuity: ContinuitySummary?
}

struct BookListingResponse: Decodable { let book: BookDetail }
struct DemoBookResponse: Decodable { let book: BookDetail }
struct BookFile: Decodable { let path: String; let content: String }

struct NewBookDraft: Encodable {
    var bookId: String
    var title: String
    var genre: String
    var subgenre = ""
    var platform = ""
    var audience = ""
    var length = ""
    var chapterChars = ""
    var idea = ""
    var tone = ""
    var pleasure = ""
    var protagonist = ""
    var conflict = ""
    var ending = ""
    var taboos = ""
    var references = ""
    var pov = ""
}

// MARK: - Chapters (prose parsing)

struct Chapter: Identifiable, Hashable {
    let number: Int
    let title: String
    let body: String
    var id: Int { number }
    var wordCount: Int { body.unicodeScalars.filter { !CharacterSet.whitespacesAndNewlines.contains($0) }.count }

    static func parse(_ markdown: String) -> [Chapter] {
        let lines = markdown.replacingOccurrences(of: "\r\n", with: "\n").split(separator: "\n", omittingEmptySubsequences: false)
        var result: [Chapter] = []
        var number: Int?
        var title = ""
        var body: [Substring] = []
        func flush() {
            guard let number else { return }
            result.append(Chapter(number: number, title: title, body: body.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)))
        }
        for line in lines {
            if let match = try? /^##\s*第\s*(\d+)\s*章\s*(.*)$/.wholeMatch(in: line) {
                flush()
                number = Int(match.1)
                title = String(match.2).trimmingCharacters(in: .whitespaces)
                body = []
            } else if number != nil, line.trimmingCharacters(in: .whitespaces) != "---" {
                body.append(line)
            }
        }
        flush()
        return result
    }

    /// Replaces one chapter inside the unit's raw markdown, keeping every other
    /// byte intact (the desktop saveChapterEdit algorithm). Returns nil when the
    /// chapter heading cannot be located.
    static func replaceChapter(_ number: Int, in original: String, with edited: String) -> String? {
        let normalized = original.replacingOccurrences(of: "\r\n", with: "\n")
        let lines = normalized.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        let heading = try! Regex("^##\\s*第\\s*\(number)\\s*章(?:\\s.*)?$")
        let anyHeading = try! Regex("^##\\s*第\\s*\\d+\\s*章(?:\\s.*)?$")
        guard let start = lines.firstIndex(where: { (try? heading.wholeMatch(in: $0)) != nil }) else { return nil }
        var end = lines.count
        for index in (start + 1)..<lines.count {
            if (try? anyHeading.wholeMatch(in: lines[index])) != nil { end = index; break }
        }
        let before = lines[..<start].joined(separator: "\n")
        let after = lines[end...].joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
        var result = before.isEmpty ? "" : before + "\n"
        result += edited.trimmingCharacters(in: .whitespacesAndNewlines) + "\n\n"
        result += after
        return result
    }
}

// MARK: - Runs

struct TaskSpec: Codable, Hashable {
    var kind: String
    var bookId: String
    var volumeId: String?
    var unitId: String?
    var chapter: Int?
    var baseRevision: String
    var modelId: String?
    var idempotencyKey: String?
    var knowledgeIds: [String]?
    var selection: TextSelection?
    var instruction: String?
    var issueIds: [String]?
    var rewriteMode: String?
    var targetPath: String?
    var entityKind: String?
    var mode: String?
    var preservePaths: [String]?
    var forceRiskAccept: Bool?
    var supersedeReview: Bool?
}

struct TextSelection: Codable, Hashable {
    var path: String
    var start: Int
    var end: Int
    var text: String
    var before: String?
    var after: String?
}

struct CallPolicy: Decodable, Hashable {
    let maxCalls: Int
    let timeoutSec: Int?
    let allowOneRepair: Bool?
}

struct CallRecord: Decodable, Hashable, Identifiable {
    let index: Int
    let modelId: String
    let provider: String
    let step: String
    let promptTokens: Int
    let completionTokens: Int
    let cacheReadTokens: Int?
    let cacheCreationTokens: Int?
    let finishReason: String?
    let reasoningTokens: Int?
    let durationMs: Int64
    let error: String?
    let startedAt: String?

    var id: Int { index }
}

/// Full run record from /api/runs. `RunRecord` below stays as the lightweight
/// list row so old decoding call sites keep compiling.
struct Run: Decodable, Identifiable, Hashable {
    let id: String
    let bookId: String
    let kind: String
    let status: String
    let spec: TaskSpec?
    let policy: CallPolicy?
    let modelId: String
    let callsUsed: Int
    let calls: [CallRecord]?
    let candidateId: String?
    let contextSummary: [String]?
    let error: String?
    let warnings: [String]?
    let createdAt: String
    let updatedAt: String?

    // Precomputed totals from the slim run list. The full run carries calls[]
    // instead; the computed accessors below prefer whichever is present, so
    // list rows and detail views share one type.
    let summaryInputTokens: Int?
    let summaryOutputTokens: Int?
    let summaryDurationMs: Int64?
    let summaryCacheRead: Int?
    let summaryCacheCreation: Int?

    enum CodingKeys: String, CodingKey {
        case id, bookId, kind, status, spec, policy, modelId, callsUsed, calls
        case candidateId, contextSummary, error, warnings, createdAt, updatedAt
        case summaryInputTokens = "inputTokens"
        case summaryOutputTokens = "outputTokens"
        case summaryDurationMs = "durationMs"
        case summaryCacheRead = "cacheRead"
        case summaryCacheCreation = "cacheCreation"
    }

    var kindTitle: String { Copy.runKindLabel(kind, mode: spec?.rewriteMode ?? spec?.mode) }
    var statusTitle: String { Copy.runStatus(status) }
    var isTerminated: Bool { status != "running" && status != "pending" }

    var inputTokens: Int { summaryInputTokens ?? (calls ?? []).reduce(0) { $0 + $1.promptTokens } }
    var outputTokens: Int { summaryOutputTokens ?? (calls ?? []).reduce(0) { $0 + $1.completionTokens } }
    var durationMs: Int64 { summaryDurationMs ?? (calls ?? []).reduce(0) { $0 + $1.durationMs } }
    var cache: CacheStat {
        if let read = summaryCacheRead, let prompt = summaryInputTokens {
            let creation = summaryCacheCreation ?? 0
            return CacheStat(
                read: read, creation: creation, prompt: prompt,
                ratio: prompt > 0 ? Double(read) / Double(prompt) : 0,
                hasData: read > 0 || creation > 0
            )
        }
        return CacheStat.of(calls)
    }

    /// Same target resolution as the desktop runs table.
    var target: String {
        guard let spec else { return bookId }
        if kind == "plan" {
            if spec.mode == "volume" || spec.volumeId != nil { return spec.volumeId ?? Copy.runTargetCurrentVolume }
            return Copy.runTargetWholeBook
        }
        if kind == "idea" { return "outline/idea.md" }
        if kind == "rewrite_entity" { return spec.targetPath ?? Copy.contentBible }
        if kind == "outline", let volume = spec.volumeId, spec.unitId == nil { return volume }
        if let unit = spec.unitId { return unit }
        if let volume = spec.volumeId { return volume }
        if let chapter = spec.chapter { return Copy.chapterHeading(chapter) }
        return Copy.runTargetWholeBook
    }
}

/// Lightweight row for the logs list (kept for source compatibility).
struct RunRecord: Decodable, Identifiable {
    let id: String
    let bookId: String
    let kind: String
    let status: String
    let modelId: String
    let callsUsed: Int
    let error: String?
    let createdAt: String

    var kindTitle: String { Copy.runKind(kind) }
    var statusTitle: String { Copy.runStatus(status) }
}

struct CacheStat: Hashable {
    let read: Int
    let creation: Int
    let prompt: Int
    let ratio: Double
    let hasData: Bool

    static let empty = CacheStat(read: 0, creation: 0, prompt: 0, ratio: 0, hasData: false)

    static func of(_ calls: [CallRecord]?) -> CacheStat {
        guard let calls, !calls.isEmpty else { return .empty }
        var read = 0, creation = 0, prompt = 0, hasData = false
        for call in calls {
            let r = call.cacheReadTokens ?? 0
            let w = call.cacheCreationTokens ?? 0
            if r > 0 || w > 0 { hasData = true }
            read += r
            creation += w
            prompt += call.promptTokens
        }
        return CacheStat(read: read, creation: creation, prompt: prompt, ratio: prompt > 0 ? Double(read) / Double(prompt) : 0, hasData: hasData)
    }

    var hitRateText: String { hasData ? "\(Int((ratio * 100).rounded()))%" : "—" }
}

// MARK: - Context preview

struct ContextBlock: Decodable, Hashable, Identifiable {
    let id: String
    let label: String
    let source: String
    let content: String
    let bytes: Int
    let optional: Bool?
    let omitted: Bool?
    let omitReason: String?
}

struct ContextPacket: Decodable, Hashable {
    let contractVersion: String?
    let taskKind: String
    let bookId: String
    let unitId: String?
    let volumeId: String?
    let blocks: [ContextBlock]
    let estPromptTokens: Int?
    let policy: CallPolicy
    let systemPrompt: String
    let userPrompt: String

    var totalBytes: Int { blocks.reduce(0) { $0 + $1.bytes } }
    var basisBlocks: [ContextBlock] { blocks.filter { $0.id.hasPrefix("basis-") } }
    var contentBlocks: [ContextBlock] { blocks.filter { !$0.id.hasPrefix("basis-") } }
}

// MARK: - Candidates

struct ChapterDraft: Codable, Hashable, Identifiable {
    let chapter: Int
    let title: String
    let body: String
    var id: Int { chapter }
}

struct ReviewFinding: Codable, Hashable, Identifiable {
    let id: String
    let severity: String
    let source: String
    let lens: String?
    let title: String
    let evidence: String?
    let anchor: String?
    let roundsHit: Int?
    let chapter: Int?
    let suggestion: String?
    let entityPath: String?
    let volumeId: String?
    let unitId: String?
}

struct ChapterActualDelta: Codable, Hashable {
    let stem: String
    let field: String
    let from: String
    let to: String
    let evidence: String
    let confirmed: Bool?
}

struct ChapterActual: Codable, Hashable, Identifiable {
    let chapter: Int
    let storyDay: Int?
    let endDay: Int?
    let summary: String
    let stateDeltas: [ChapterActualDelta]?
    let plant: [String]?
    let payoff: [String]?
    let plannedForeshadowHandled: [String]?
    var id: Int { chapter }
}

struct CanonChange: Codable, Hashable {
    let path: String
    let description: String
    let before: String?
    let after: String?
    let locked: Bool?
}

/// A loose unit-outline draft: the desktop renders it as JSON; typed accessors
/// cover the common fields without pinning the whole evolving contract.
struct UnitOutlineDraft: Codable, Hashable, Identifiable {
    let fields: [String: JSONValue]

    var id: String { string("unitId") ?? UUID().uuidString }
    var unitId: String? { string("unitId") }
    var title: String? { string("title") }

    func string(_ key: String) -> String? {
        switch fields[key] {
        case .string(let value)?: return value
        case .number(let value)?:
            // Chapter counts arrive as numbers; render them without a trailing .0.
            return value == value.rounded() ? String(Int(value)) : String(value)
        default: return nil
        }
    }

    func int(_ key: String) -> Int? {
        switch fields[key] {
        case .number(let value)?: return Int(value)
        case .string(let value)?: return Int(value)
        default: return nil
        }
    }

    init(from decoder: Decoder) throws {
        fields = try [String: JSONValue](from: decoder)
    }

    func encode(to encoder: Encoder) throws {
        try fields.encode(to: encoder)
    }

    var prettyJSON: String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        guard let data = try? encoder.encode(fields) else { return "" }
        return String(decoding: data, as: UTF8.self)
    }

    /// A YAML-shaped rendering of the draft so the contract parser (which reads
    /// the on-disk snake_case YAML) can render a candidate the same way it
    /// renders an accepted outline. Only the keys the contract view reads are
    /// emitted; nested chapters/scenes are serialized as YAML blocks.
    var yamlLike: String {
        var lines: [String] = []
        // camelCase JSON keys → snake_case YAML keys the parser expects.
        let scalars: [(String, String)] = [
            ("unitId", "unit_id"), ("title", "title"), ("function", "function"),
            ("desire", "desire"), ("obstacle", "obstacle"), ("choice", "choice"),
            ("payoff", "payoff"), ("pleasure", "pleasure"), ("forbidden", "forbidden"),
            ("pov", "pov"), ("storyDay", "story_day"), ("endDay", "end_day"),
            ("gapDays", "gap_days"), ("gapFromPrev", "gap_from_prev"),
            ("flashback", "flashback"), ("infoControl", "info_control"),
            ("chapterChars", "chapter_chars"), ("wordTarget", "word_target"),
        ]
        for (json, yaml) in scalars {
            guard let value = string(json), !value.isEmpty else { continue }
            lines.append("\(yaml): \(value)")
        }
        if case .array(let items)? = fields["onStage"] {
            let names = items.compactMap { item -> String? in
                if case .string(let value) = item { return value }
                return nil
            }
            if !names.isEmpty { lines.append("on_stage: [\(names.joined(separator: ", "))]") }
        }
        if case .object(let hook)? = fields["nextHook"] {
            lines.append("next_hook:")
            for key in hook.keys.sorted() {
                if case .string(let value) = hook[key] { lines.append("  \(key): \(value)") }
            }
        }
        if case .array(let chapters)? = fields["chapters"], !chapters.isEmpty {
            lines.append("chapters:")
            for chapter in chapters {
                guard case .object(let map) = chapter else { continue }
                lines.append("  - chapter: \(Self.scalarText(map["chapter"]))")
                for key in ["goal", "change", "cut_hook", "cutHook", "day_offset", "dayOffset"] {
                    guard let value = map[key] else { continue }
                    let yamlKey = key == "cutHook" ? "cut_hook" : key == "dayOffset" ? "day_offset" : key
                    lines.append("    \(yamlKey): \(Self.scalarText(value))")
                }
                if case .array(let scenes)? = map["scenes"], !scenes.isEmpty {
                    lines.append("    scenes:")
                    for scene in scenes {
                        guard case .object(let sceneMap) = scene else { continue }
                        var first = true
                        for key in ["id", "chapter", "goal", "conflict", "turn", "outcome", "who", "where"] {
                            guard let value = sceneMap[key] else { continue }
                            let text = Self.scalarText(value)
                            guard !text.isEmpty else { continue }
                            lines.append(first ? "      - \(key): \(text)" : "        \(key): \(text)")
                            first = false
                        }
                        for key in ["mustLand", "reveal", "withhold"] {
                            guard case .array(let items)? = sceneMap[key] else { continue }
                            let texts = items.compactMap { item -> String? in
                                if case .string(let value) = item { return value }
                                return nil
                            }
                            guard !texts.isEmpty else { continue }
                            let yamlKey = key == "mustLand" ? "must_land" : key
                            lines.append("        \(yamlKey): [\(texts.joined(separator: ", "))]")
                        }
                    }
                }
            }
        }
        return lines.joined(separator: "\n")
    }

    private static func scalarText(_ value: JSONValue?) -> String {
        switch value {
        case .string(let text)?: return text
        case .number(let number)?: return number == number.rounded() ? String(Int(number)) : String(number)
        case .bool(let flag)?: return flag ? "true" : "false"
        case .array(let items)?:
            let texts = items.compactMap { item -> String? in
                if case .string(let text) = item { return text }
                return nil
            }
            return "[\(texts.joined(separator: ", "))]"
        default: return ""
        }
    }
}

struct Candidate: Decodable, Identifiable, Hashable {
    let id: String
    let runId: String
    let bookId: String
    let kind: String
    let status: String
    let baseRevision: String
    let targetPath: String?
    let unitId: String?
    let volumeId: String?
    let chapters: [ChapterDraft]?
    let baseChapters: [ChapterDraft]?
    let unitOutlines: [UnitOutlineDraft]?
    let mode: String?
    let preservePaths: [String]?
    let planMarkdown: [String: String]?
    let planSettings: [String: String]?
    let actualChapters: [ChapterActual]?
    let replacement: String?
    let findings: [ReviewFinding]?
    let canonChanges: [CanonChange]?
    let createdAt: String
    let acceptedAt: String?

    // Slim-list flags: which bulk payloads the detail endpoint carries.
    // nil on a full candidate (the payloads themselves are the source of truth).
    let hasChapters: Bool?
    let hasBaseChapters: Bool?
    let hasPlan: Bool?
    let hasReplacement: Bool?
    let hasOutlines: Bool?
    let hasActual: Bool?

    /// True when this row came from the slim list and its payloads must be
    /// fetched from /api/candidates/:id before rendering.
    var isSummary: Bool { hasChapters != nil }
    var needsDetail: Bool {
        guard isSummary else { return false }
        return (hasChapters ?? false) || (hasBaseChapters ?? false) || (hasPlan ?? false)
            || (hasReplacement ?? false) || (hasOutlines ?? false) || (hasActual ?? false)
    }

    var kindTitle: String {
        if kind == "plan" { return Copy.planMode(mode) }
        if kind == "write", (baseChapters?.isEmpty == false) || (hasBaseChapters ?? false) { return Copy.kindWriteRefine }
        return Copy.runKind(kind)
    }

    /// Target label, mirroring the desktop candidateTarget().
    func targetLabel() -> String {
        if let unitId { return unitId }
        if let targetPath { return targetPath }
        let outlines = unitOutlines ?? []
        if outlines.count == 1, let id = outlines[0].unitId { return id }
        if outlines.count > 1 { return "\(volumeId ?? "") · \(Copy.candidateUnits(outlines.count))" }
        return volumeId ?? ""
    }

    /// Finalize advisories whose evidence quote was not found in the prose.
    var unverifiedEvidence: [ReviewFinding] {
        (findings ?? []).filter { $0.id.hasPrefix("evidence-") }
    }
}

struct AcceptCommand: Encodable {
    var baseRevision: String
    var acceptFindingIds: [String]?
    var forceRiskAccept: Bool?
}

// MARK: - Knowledge

struct KnowledgeDoc: Decodable, Identifiable, Hashable {
    let id: String
    let title: String
    let summary: String
    let bytes: Int
    let category: String
    let builtin: Bool
    let customized: Bool
    let stages: [String]?
    let genres: [String]?
    let subgenres: [String]?
    let isDefault: Bool?
    let audience: String?

    enum CodingKeys: String, CodingKey {
        case id, title, summary, bytes, category, builtin, customized, stages, genres, subgenres, audience
        case isDefault = "default"
    }

    var isAuthorDoc: Bool { audience == "author" }
}

struct KnowledgeContent: Decodable {
    let id: String
    let content: String
    let stages: [String]?
    let genres: [String]?
    let subgenres: [String]?
    let isDefault: Bool?
    let audience: String?

    enum CodingKeys: String, CodingKey {
        case id, content, stages, genres, subgenres, audience
        case isDefault = "default"
    }
}

struct KnowledgeUpdate: Encodable {
    let content: String
    let stages: [String]
    let genres: [String]
    let subgenres: [String]
}

// MARK: - Providers / settings

struct Provider: Decodable, Identifiable {
    let id: String
    let name: String
    let provider: String
    let baseUrl: String?
    let defaultProtocolDialect: String?
    let hasApiKey: Bool
    let models: [ProviderModel]
}

struct ProviderModel: Codable, Hashable {
    var name: String
    var enabled: Bool
    var protocolDialect: String?

    init(name: String, enabled: Bool = true, protocolDialect: String? = nil) {
        self.name = name
        self.enabled = enabled
        self.protocolDialect = protocolDialect
    }
}

struct ProviderRequest: Encodable {
    var provider: String
    var name: String
    var apiKey: String
    var baseUrl: String?
    var defaultProtocolDialect: String?
    var models: [ProviderModel]
}

struct ProviderPreset: Decodable, Identifiable, Hashable {
    let id: String
    let name: String
    let provider: String
    let baseUrl: String?
    let defaultProtocolDialect: String?
    let description: String?
}

struct LLMModel: Decodable, Identifiable, Hashable {
    let id: String
    let name: String
    let providerId: String
    let provider: String
    let enabled: Bool
    let availableEfforts: [String]?
    let vision: Bool?
    let protocolDialect: String?
}

struct ModelAssignment: Codable, Hashable {
    var role: String
    var modelId: String
}

/// Per-model capacity / sampling / reasoning catalog entry. Snake-case keys
/// mirror the Go JSON contract.
struct ModelConfig: Codable, Hashable, Identifiable {
    var model: String
    var contextWindow: Int?
    var maxOutput: Int?
    var temperature: Double?
    var topP: Double?
    var frequencyPenalty: Double?
    var presencePenalty: Double?
    var stop: [String]?
    var availableEfforts: [String]?
    var vision: Bool?

    var id: String { model }

    enum CodingKeys: String, CodingKey {
        case model
        case contextWindow = "context_window"
        case maxOutput = "max_output"
        case temperature
        case topP = "top_p"
        case frequencyPenalty = "frequency_penalty"
        case presencePenalty = "presence_penalty"
        case stop
        case availableEfforts = "available_efforts"
        case vision
    }
}

struct AppSettings: Decodable {
    let libraryRoot: String?
    let customLibraryRoot: String?
    let modelAssignments: [ModelAssignment]?
    let defaultModels: [ModelConfig]?
    let review: ReviewSettings?
}

struct ReviewSettings: Codable, Hashable {
    var concurrency: Int
    var rounds: Int
}

struct LLMTestResult: Decodable {
    let ok: Bool
    let durationMs: Int64?
    let error: String?
    let content: String?
}

struct ModelsEnvelope: Decodable { let models: [JSONValue]? }

// MARK: - Misc

struct OKResponse: Decodable { let ok: Bool? }

/// A JSON scalar/object/array used where the desktop keeps `any` (unit outline
/// drafts, fetched model lists). Round-trips through Codable.
enum JSONValue: Codable, Hashable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case object([String: JSONValue])
    case array([JSONValue])
    case null

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() { self = .null; return }
        if let value = try? container.decode(Bool.self) { self = .bool(value); return }
        if let value = try? container.decode(Double.self) { self = .number(value); return }
        if let value = try? container.decode(String.self) { self = .string(value); return }
        if let value = try? container.decode([String: JSONValue].self) { self = .object(value); return }
        if let value = try? container.decode([JSONValue].self) { self = .array(value); return }
        throw DecodingError.dataCorruptedError(in: container, debugDescription: "unsupported JSON value")
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .string(let value): try container.encode(value)
        case .number(let value): try container.encode(value)
        case .bool(let value): try container.encode(value)
        case .object(let value): try container.encode(value)
        case .array(let value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }
}