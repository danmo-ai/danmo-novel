import Foundation

/// Sentence-level LCS diff with character-level refinement, mirroring
/// `ChapterDiff.vue`, plus the line-level diff used by plan candidate previews
/// (`PlanCandidateDiff.vue`). Pure functions; the views only render segments.
enum TextDiff {
    enum Kind: Hashable { case same, add, del }

    struct Segment: Hashable, Identifiable {
        let kind: Kind
        let text: String
        var id: String { "\(kind)-\(text.hashValue)" }
    }

    struct ChapterRow: Identifiable {
        let chapter: Int
        let title: String
        let segments: [Segment]
        let changed: Bool
        let added: Int
        let removed: Int
        var id: Int { chapter }
    }

    // MARK: Sentence diff

    /// Chinese prose edits land on sentences, so sentences are the reading
    /// unit for the first pass (same as desktop).
    static func splitSentences(_ text: String) -> [String] {
        var out: [String] = []
        var buffer = ""
        for character in text {
            buffer.append(character)
            if "。！？；\n".contains(character) {
                out.append(buffer)
                buffer = ""
            }
        }
        if !buffer.isEmpty { out.append(buffer) }
        return out
    }

    private static func pushMerge(_ segments: inout [Segment], _ kind: Kind, _ text: String, joiner: String = "") {
        guard !text.isEmpty else { return }
        if let last = segments.last, last.kind == kind {
            segments[segments.count - 1] = Segment(kind: kind, text: last.text + joiner + text)
        } else {
            segments.append(Segment(kind: kind, text: text))
        }
    }

    /// Generic LCS over token arrays; returns interleaved same/del/add.
    static func lcs(_ a: [String], _ b: [String], joiner: String = "") -> [Segment] {
        let n = a.count
        let m = b.count
        // dp[i][j] = LCS length of a[i...] and b[j...]
        var dp = Array(repeating: Array(repeating: 0, count: m + 1), count: n + 1)
        if n > 0 && m > 0 {
            for i in stride(from: n - 1, through: 0, by: -1) {
                for j in stride(from: m - 1, through: 0, by: -1) {
                    dp[i][j] = a[i] == b[j] ? dp[i + 1][j + 1] + 1 : max(dp[i + 1][j], dp[i][j + 1])
                }
            }
        }
        var ops: [Segment] = []
        var i = 0
        var j = 0
        while i < n && j < m {
            if a[i] == b[j] {
                pushMerge(&ops, .same, a[i], joiner: joiner)
                i += 1; j += 1
            } else if dp[i + 1][j] >= dp[i][j + 1] {
                pushMerge(&ops, .del, a[i], joiner: joiner)
                i += 1
            } else {
                pushMerge(&ops, .add, b[j], joiner: joiner)
                j += 1
            }
        }
        while i < n { pushMerge(&ops, .del, a[i], joiner: joiner); i += 1 }
        while j < m { pushMerge(&ops, .add, b[j], joiner: joiner); j += 1 }
        return ops
    }

    /// Character refinement inside paired del/add runs: a sentence diff repeats
    /// the whole sentence for a one-word change; this highlights only the words.
    /// Budget mirrors the desktop (skip refinement for wholesale rewrites).
    static let refineBudget = 400_000

    static func refine(_ ops: [Segment]) -> [Segment] {
        var out: [Segment] = []
        var i = 0
        while i < ops.count {
            if ops[i].kind == .same {
                pushMerge(&out, .same, ops[i].text)
                i += 1
                continue
            }
            var dels = ""
            var adds = ""
            while i < ops.count && ops[i].kind != .same {
                if ops[i].kind == .del { dels += ops[i].text } else { adds += ops[i].text }
                i += 1
            }
            let delChars = Array(dels).map(String.init)
            let addChars = Array(adds).map(String.init)
            if !dels.isEmpty && !adds.isEmpty && delChars.count * addChars.count <= refineBudget {
                for segment in lcs(delChars, addChars) { pushMerge(&out, segment.kind, segment.text) }
            } else {
                pushMerge(&out, .del, dels)
                pushMerge(&out, .add, adds)
            }
        }
        return out
    }

    static func chapterRows(base: [ChapterDraft]?, revised: [ChapterDraft]) -> [ChapterRow] {
        var baseByChapter: [Int: ChapterDraft] = [:]
        for chapter in base ?? [] { baseByChapter[chapter.chapter] = chapter }
        return revised.map { chapter in
            guard let before = baseByChapter[chapter.chapter] else {
                return ChapterRow(chapter: chapter.chapter, title: chapter.title,
                                  segments: [Segment(kind: .add, text: chapter.body)],
                                  changed: true, added: chapter.body.count, removed: 0)
            }
            if before.body == chapter.body {
                return ChapterRow(chapter: chapter.chapter, title: chapter.title,
                                  segments: [Segment(kind: .same, text: chapter.body)],
                                  changed: false, added: 0, removed: 0)
            }
            let segments = refine(lcs(splitSentences(before.body), splitSentences(chapter.body)))
            var added = 0
            var removed = 0
            for segment in segments {
                if segment.kind == .add { added += segment.text.count }
                if segment.kind == .del { removed += segment.text.count }
            }
            return ChapterRow(chapter: chapter.chapter, title: chapter.title, segments: segments,
                              changed: true, added: added, removed: removed)
        }
    }

    // MARK: Line diff (plan files)

    /// Files are a few hundred lines; beyond the cap fall back to wholesale
    /// replacement so opening a candidate never stalls the UI.
    static let maxDiffLines = 1200

    static func lineDiff(before: String, after: String) -> [Segment] {
        let a = before.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        let b = after.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        if a.count + b.count > maxDiffLines {
            var out: [Segment] = []
            if !before.isEmpty { out.append(Segment(kind: .del, text: before)) }
            if !after.isEmpty { out.append(Segment(kind: .add, text: after)) }
            return out
        }
        return lcs(a, b, joiner: "\n")
    }
}

/// Per-file change preview for a plan candidate (PlanCandidateDiff.vue).
struct PlanFileChange: Identifiable {
    enum Status: String { case new, changed, same, kept }
    let path: String
    let current: String
    let next: String
    let status: Status
    var id: String { path }

    var statusLabel: String {
        switch status {
        case .new: Copy.diffStatusNew
        case .changed: Copy.diffStatusChanged
        case .same: Copy.diffStatusSame
        case .kept: Copy.diffStatusKept
        }
    }

    static func rows(candidate: Candidate, files: [String: String]) -> [PlanFileChange] {
        let preserve = Set(candidate.preservePaths ?? [])
        var out: [PlanFileChange] = []
        func push(_ path: String, _ next: String) {
            let current = files[path] ?? ""
            let status: Status
            if preserve.contains(path) { status = .kept }
            else if current.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { status = .new }
            else if next == current { status = .same }
            else { status = .changed }
            out.append(PlanFileChange(path: path, current: current, next: next, status: status))
        }
        if candidate.mode == "volume" {
            if let outline = candidate.planMarkdown?["volume_outline"] {
                push("outline/volumes/\(candidate.volumeId ?? "v01").md", outline)
            }
            return out
        }
        if let outline = candidate.planMarkdown?["book_outline"] { push("outline/book_outline.md", outline) }
        for (path, body) in (candidate.planSettings ?? [:]).sorted(by: { $0.key < $1.key }) { push(path, body) }
        for card in castFiles(candidate.planMarkdown?["cast_json"]) { push(card.path, card.next) }
        return out
    }

    /// Files the accept will actually write (kept paths excluded).
    static func writtenPaths(candidate: Candidate) -> [String] {
        rows(candidate: candidate, files: [:]).filter { $0.status != .kept }.map(\.path)
    }

    private static func castFiles(_ raw: String?) -> [(path: String, next: String)] {
        guard let raw, let data = raw.data(using: .utf8) else { return [] }
        struct Card: Decodable { let stem: String?; let cardMarkdown: String? }
        guard let cards = try? JSONDecoder().decode([Card].self, from: data) else { return [] }
        return cards.compactMap { card in
            guard let stem = card.stem, !stem.isEmpty else { return nil }
            return (path: "canon/cast/\(stem).md", next: card.cardMarkdown ?? "# \(stem)\n")
        }
    }
}