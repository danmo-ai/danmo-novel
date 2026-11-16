import Foundation

/// Knowledge applicability rules, mirroring `knowledgeStages.ts` and the Go
/// `Doc.GenreMatched`. Kept pure so both the picker and the knowledge page
/// filter identically.
enum KnowledgeFilter {
    /// Maps a run spec onto the stage whose articles the picker offers.
    static func stage(for spec: TaskSpec) -> String {
        switch spec.kind {
        case "plan": spec.mode == "volume" ? "volume" : "plan"
        case "idea": "plan"
        case "outline": "outline"
        case "write": "write"
        case "review": "review"
        case "rewrite_entity": "entity"
        default: ""
        }
    }

    /// Untagged articles apply everywhere so a new article is never invisible.
    static func appliesTo(stages: [String]?, stage: String) -> Bool {
        guard !stage.isEmpty, let stages, !stages.isEmpty else { return true }
        return stages.contains(stage)
    }

    static func isCommon(_ doc: KnowledgeDoc) -> Bool {
        (doc.genres ?? []).isEmpty && (doc.subgenres ?? []).isEmpty
    }

    /// Same alias table as the backend: platform categories and broad words
    /// must still reach the matching family.
    static let genreAliases: [String: [String]] = [
        "玄幻": ["玄幻"], "奇幻": ["玄幻"], "东方玄幻": ["玄幻"], "西方奇幻": ["玄幻"],
        "仙侠": ["仙侠"], "修真": ["仙侠"], "武侠": ["仙侠"], "凡人修仙": ["仙侠"], "古典仙侠": ["仙侠"],
        "都市": ["都市"], "现实": ["都市"], "都市日常": ["都市"], "都市高武": ["都市"], "战神赘婿": ["都市"],
        "悬疑": ["悬疑"], "推理": ["悬疑"], "刑侦": ["悬疑"], "灵异": ["悬疑"], "悬疑灵异": ["悬疑"],
        "言情": ["现代言情", "古代言情"],
        "现代言情": ["现代言情"], "都市言情": ["现代言情"],
        "古代言情": ["古代言情"], "古言": ["古代言情"], "宫斗": ["古代言情"],
        "仕途扫黑": ["仕途扫黑"], "仕途": ["仕途扫黑"], "官场": ["仕途扫黑"], "扫黑": ["仕途扫黑"],
        "系统穿越": ["系统穿越"], "系统": ["系统穿越"], "签到": ["系统穿越"], "快穿": ["系统穿越"], "穿越": ["系统穿越"],
    ]

    static func genreFamilies(_ name: String?) -> [String] {
        let trimmed = (name ?? "").trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return [] }
        if let alias = genreAliases[trimmed] { return alias }
        var families: [String] = []
        func add(_ list: [String]) {
            for family in list where !families.contains(family) { families.append(family) }
        }
        for family in Copy.knowledgeGenres where trimmed.contains(family) { add([family]) }
        for (key, list) in genreAliases where key != trimmed && trimmed.contains(key) { add(list) }
        let ordered = Copy.knowledgeGenres.filter { families.contains($0) }
        return ordered.isEmpty ? [trimmed] : ordered
    }

    static let subgenreAliases: [String: String] = [
        "刑侦探案": "刑侦探案", "刑侦": "刑侦探案", "年代刑侦": "刑侦探案", "探案": "刑侦探案",
        "侦探推理": "刑侦探案", "侦探": "刑侦探案", "警察": "刑侦探案", "法医": "刑侦探案", "社会派": "刑侦探案",
    ]

    static func canonicalSubgenre(_ value: String?) -> String {
        let trimmed = (value ?? "").trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return "" }
        if let canonical = subgenreAliases[trimmed] { return canonical }
        for (alias, canonical) in subgenreAliases where alias != canonical && trimmed.contains(alias) {
            return canonical
        }
        return ""
    }

    static func subgenreMatched(_ tagged: String, _ book: String) -> Bool {
        let t = canonicalSubgenre(tagged)
        let b = canonicalSubgenre(book)
        if !t.isEmpty && !b.isEmpty { return t == b }
        return tagged.trimmingCharacters(in: .whitespaces).lowercased() == book.trimmingCharacters(in: .whitespaces).lowercased()
    }

    /// 公共 always matches; genre tags compare by family; subgenre tags are an
    /// extra restriction.
    static func appliesToGenre(_ doc: KnowledgeDoc, bookGenre: String?, bookSubgenre: String?) -> Bool {
        if isCommon(doc) { return true }
        if let genres = doc.genres, !genres.isEmpty {
            let book = genreFamilies(bookGenre)
            let hit = genres.contains { tag in genreFamilies(tag).contains(where: { book.contains($0) }) }
            if !hit { return false }
        }
        if let subgenres = doc.subgenres, !subgenres.isEmpty {
            let sub = (bookSubgenre ?? "").trimmingCharacters(in: .whitespaces)
            if sub.isEmpty { return false }
            return subgenres.contains { subgenreMatched($0, sub) }
        }
        return true
    }

    static func stageSummary(_ stages: [String]?) -> String {
        guard let stages, !stages.isEmpty else { return Copy.knowledgeStagesUntaggedShort }
        return stages.map { Copy.knowledgeStageLabel($0) }.joined(separator: " · ")
    }

    static func genreSummary(_ doc: KnowledgeDoc) -> String {
        if isCommon(doc) { return Copy.knowledgeGenreSummaryCommon }
        var parts = doc.genres ?? []
        if let subgenres = doc.subgenres, !subgenres.isEmpty {
            parts.append(Copy.knowledgeGenreOnly(subgenres.joined(separator: "、")))
        }
        return parts.joined(separator: " · ")
    }

    /// Why a row is greyed out when the author chooses to see everything.
    static func mismatchReasons(_ doc: KnowledgeDoc, stage: String, bookGenre: String?, bookSubgenre: String?) -> String {
        var reasons: [String] = []
        if !appliesTo(stages: doc.stages, stage: stage) { reasons.append(Copy.knowledgeMismatchStage) }
        if !appliesToGenre(doc, bookGenre: bookGenre, bookSubgenre: bookSubgenre) { reasons.append(Copy.knowledgeMismatchGenre) }
        return reasons.joined(separator: " / ")
    }
}