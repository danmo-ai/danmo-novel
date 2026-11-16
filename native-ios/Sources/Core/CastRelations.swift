import Foundation

/// Parses formal `## 关系` tables plus 关系锚点 / 与主角关系 prose from cast
/// cards, mirroring `castRelations.ts`.
struct CastRelation: Hashable, Identifiable {
    let from: String
    let to: String
    let type: String
    let current: String
    let formal: Bool
    var id: String { [from, to].sorted().joined(separator: "|") }
}

enum CastRelations {
    static func normalizePersonKey(_ value: String) -> String {
        var text = value
        if text.hasPrefix("# ") { text = String(text.dropFirst(2)) }
        if let index = text.firstIndex(where: { $0 == "（" || $0 == "(" }) {
            text = String(text[..<index])
        }
        return text.lowercased().filter { !"` -_/".contains($0) }
    }

    static func resolveRef(_ ref: String, cards: [CastCard]) -> String {
        let cleaned = ref.filter { !"`\"'‘’".contains($0) }
            .trimmingCharacters(in: .whitespaces)
        let trimmed = cleaned.hasSuffix("等") ? String(cleaned.dropLast()) : cleaned
        guard !trimmed.isEmpty else { return "" }
        if cards.contains(where: { $0.stem == trimmed }) { return trimmed }
        let want = normalizePersonKey(trimmed)
        guard !want.isEmpty else { return "" }
        for card in cards {
            if normalizePersonKey(card.name) == want || normalizePersonKey(card.stem) == want { return card.stem }
        }
        var hit = ""
        for card in cards {
            let name = normalizePersonKey(card.name)
            guard !name.isEmpty else { continue }
            if name.hasPrefix(want) || want.hasPrefix(name) {
                if !hit.isEmpty && hit != card.stem { return "" }
                hit = card.stem
            }
        }
        return hit
    }

    static func isProtagonist(_ role: String) -> Bool {
        ["protagonist", "hero", "main"].contains(role.lowercased())
    }

    private static func tableCells(_ line: String) -> [String] {
        line.trimmingCharacters(in: .whitespaces)
            .trimmingCharacters(in: CharacterSet(charactersIn: "|"))
            .split(separator: "|", omittingEmptySubsequences: false)
            .map { $0.trimmingCharacters(in: .whitespaces) }
    }

    private static func parseFormal(_ raw: String, from: String, cards: [CastCard]) -> [CastRelation] {
        guard let section = Rx.first("##\\s*(?:关系|人物关系)[^\\n]*\\n([\\s\\S]*?)(?=\\n##\\s|$)", in: raw, multiline: false) else { return [] }
        var out: [CastRelation] = []
        for line in section[1].split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard trimmed.hasPrefix("|") else { continue }
            if Rx.matches("^\\s*\\|?\\s*:?-+", in: line, multiline: false) { continue }
            let cells = tableCells(line)
            if cells.count < 2 || ["对方", "角色", "人物", "stem"].contains(cells[0]) { continue }
            let to = resolveRef(cells[0], cards: cards)
            if to.isEmpty || to == from { continue }
            out.append(CastRelation(from: from, to: to, type: cells[1], current: cells.count > 2 ? cells[2] : "", formal: true))
        }
        return out
    }

    private static func splitNamesAndDesc(_ part: String) -> (names: [String], desc: String) {
        var head = part
        var desc = ""
        if let open = part.firstIndex(where: { $0 == "（" || $0 == "(" }) {
            let afterOpen = part.index(after: open)
            if let close = part[afterOpen...].firstIndex(where: { $0 == "）" || $0 == ")" }) {
                head = String(part[..<open]).trimmingCharacters(in: .whitespaces)
                desc = String(part[afterOpen..<close]).trimmingCharacters(in: .whitespaces)
            }
        }
        let names = head.replacingOccurrences(of: "、", with: ",")
            .replacingOccurrences(of: "，", with: ",")
            .replacingOccurrences(of: "/", with: ",")
            .split(separator: ",")
            .map { item -> String in
                let text = item.trimmingCharacters(in: .whitespaces)
                return text.hasSuffix("等") ? String(text.dropLast()) : text
            }
            .filter { !$0.isEmpty }
        return (names, desc)
    }

    private static func parseAnchor(_ raw: String, from: String, cards: [CastCard]) -> [CastRelation] {
        guard let match = Rx.first("^[\\t *>-]*\\*{0,2}关系锚点\\*{0,2}[：:]\\s*(.+)$", in: raw) else { return [] }
        var out: [CastRelation] = []
        let parts = match[1].replacingOccurrences(of: ";", with: "；")
            .split(separator: "；").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        for part in parts {
            let (names, desc) = splitNamesAndDesc(part)
            let type = desc.isEmpty ? "关系锚点" : String(desc.prefix(16))
            for name in names {
                let to = resolveRef(name, cards: cards)
                if to.isEmpty || to == from { continue }
                out.append(CastRelation(from: from, to: to, type: type, current: desc, formal: false))
            }
        }
        return out
    }

    static func parse(cards: [CastCard]) -> [CastRelation] {
        var result: [CastRelation] = []
        var seen: Set<String> = []
        func add(_ relation: CastRelation) {
            guard !relation.from.isEmpty, !relation.to.isEmpty, relation.from != relation.to else { return }
            if seen.contains(relation.id) { return }
            seen.insert(relation.id)
            result.append(relation)
        }
        let protagonist = cards.first { isProtagonist($0.role) }
        for card in cards {
            let raw = card.raw ?? ""
            for relation in parseFormal(raw, from: card.stem, cards: cards) { add(relation) }
            for relation in parseAnchor(raw, from: card.stem, cards: cards) { add(relation) }
            if let protagonist, card.stem != protagonist.stem {
                let text = Rx.first("^[\\t *>-]*\\*{0,2}(?:与主角关系|关键关系|关系钩)\\*{0,2}[：:]\\s*(.+)$", in: raw)?[1].trimmingCharacters(in: .whitespaces)
                    ?? Rx.first("(?:与主角关系|关键关系|关系钩)[：:]\\s*(.+)", in: raw, multiline: false)?[1].trimmingCharacters(in: .whitespaces)
                if let text, !text.isEmpty {
                    add(CastRelation(from: card.stem, to: protagonist.stem, type: "与主角", current: text, formal: false))
                }
            }
        }
        return result
    }
}