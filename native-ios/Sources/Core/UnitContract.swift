import Foundation

/// Reads the unit outline YAML into the contract view model, mirroring
/// `UnitContract.vue`. Regex-based like the desktop: the outline is YAML, but
/// the contract view only needs scalars, inline lists and the chapters/scenes
/// blocks, and this keeps the phone and desktop reading the same shapes.
struct UnitContract {
    struct Field: Hashable, Identifiable { let label: String; let value: String; var id: String { label } }
    struct Scene: Hashable, Identifiable {
        var id: String
        var chapter: String
        var goal = ""
        var conflict = ""
        var turn = ""
        var outcome = ""
        var who = ""
        var place = ""
        var info = ""
        var hasBody: Bool { !goal.isEmpty || !conflict.isEmpty || !turn.isEmpty || !outcome.isEmpty }
    }
    struct ChapterPlan: Hashable, Identifiable {
        var chapter: String
        var goal = ""
        var change = ""
        var cutHook = ""
        var dayOffset = "0"
        var scenes: [Scene] = []
        var id: String { chapter }
    }
    struct OnStageIdentity: Hashable, Identifiable {
        let stem: String
        let name: String
        let identity: String
        var id: String { stem }
    }

    var fields: [Field] = []
    var meta: [String] = []
    var onStage: [String] = []
    var onStageIdentities: [OnStageIdentity] = []
    var gapFromPrev = ""
    var infoControl = ""
    var chapterPlans: [ChapterPlan] = []
    var orphanSceneGroups: [(chapter: String, scenes: [Scene])] = []

    var hasContent: Bool { !fields.isEmpty || !chapterPlans.isEmpty }

    // MARK: YAML helpers

    static func scalar(_ raw: String, _ key: String) -> String {
        guard let match = Rx.first("^\(Rx.escape(key)):\\s*(.*)$", in: raw) else { return "" }
        return unquote(match[1])
    }

    static func section(_ raw: String, _ key: String) -> String {
        guard let match = Rx.first("^\(Rx.escape(key)):\\s*\\n((?:[ \\t]+.*\\n?)*)", in: raw) else { return "" }
        return match[1]
    }

    static func inlineList(_ raw: String, _ key: String) -> String {
        guard let match = Rx.first("^\(Rx.escape(key)):\\s*\\[([^\\]]*)\\]", in: raw) else { return "" }
        return match[1].replacingOccurrences(of: "'", with: "").replacingOccurrences(of: "\"", with: "").trimmingCharacters(in: .whitespaces)
    }

    static func unquote(_ value: String) -> String {
        value.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "'\""))
    }

    static func isBareNumber(_ value: String) -> Bool {
        Rx.whole("\\d+(?:\\.\\d+)?", in: value.trimmingCharacters(in: .whitespaces)) != nil
    }

    static func indentOf(_ line: String) -> Int {
        line.prefix { $0 == " " || $0 == "\t" }.count
    }

    // MARK: Build

    init(unit: UnitInfo, book: BookSnapshot?) {
        let raw = unit.outlineRaw ?? ""
        let pairs: [(String, String)] = [
            (Copy.contractFunction, Self.scalar(raw, "function")),
            (Copy.contractDesire, Self.scalar(raw, "desire")),
            (Copy.contractObstacle, Self.scalar(raw, "obstacle")),
            (Copy.contractChoice, Self.scalar(raw, "choice")),
            (Copy.contractPayoff, Self.scalar(raw, "payoff")),
            (Copy.contractPleasure, Self.scalar(raw, "pleasure")),
            (Copy.contractForbidden, Self.scalar(raw, "forbidden")),
        ]
        fields = pairs.filter { !$0.1.isEmpty }.map { Field(label: $0.0, value: $0.1) }

        onStage = Self.inlineList(raw, "on_stage")
            .split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        infoControl = Self.scalar(raw, "info_control")

        var metaItems: [String] = []
        if unit.chapterFrom > 0 { metaItems.append(Copy.unitChapters(from: unit.chapterFrom, to: unit.chapterTo)) }
        let pov = Self.scalar(raw, "pov")
        if !pov.isEmpty { metaItems.append(Copy.contractPOV(book?.castName(pov) ?? pov)) }
        if let target = unit.chapterTarget, target > 0 {
            let chapters = max(1, unit.chapterTo - unit.chapterFrom + 1)
            metaItems.append(Copy.contractWordTarget(perChapter: target, total: target * chapters))
        }
        let day = Self.scalar(raw, "story_day")
        if !day.isEmpty, day != "null" { metaItems.append(Copy.contractStoryDay(day)) }
        if let gap = Self.gapIntervalDays(raw) {
            metaItems.append(gap == 0 ? Copy.contractFollowsPrev : Copy.contractGapDays(gap))
        }
        if Self.scalar(raw, "flashback") == "true" { metaItems.append(Copy.contractFlashback) }
        if let hookMatch = Rx.first("type:\\s*(.*)", in: Self.section(raw, "next_hook"), multiline: false) {
            let hook = Self.unquote(hookMatch[1])
            if !hook.isEmpty { metaItems.append(Copy.contractNextHook(hook)) }
        }
        meta = metaItems

        let gapValue = Self.scalar(raw, "gap_from_prev")
        gapFromPrev = Self.isBareNumber(gapValue) ? "" : gapValue

        if let book {
            onStageIdentities = onStage.compactMap { stem in
                let identity = book.identityAt(stem: stem, unitID: unit.unitId)
                guard !identity.isEmpty else { return nil }
                return OnStageIdentity(stem: stem, name: book.castName(stem), identity: identity)
            }
        }

        let unitScenes = Self.parseUnitScenes(Self.section(raw, "scenes"))
        var chapters = Self.parseChapters(Self.section(raw, "chapters"))
        if !chapters.isEmpty {
            let nested = Set(chapters.filter { !$0.scenes.isEmpty }.map(\.chapter))
            for scene in unitScenes where !nested.contains(scene.chapter) {
                if let index = chapters.firstIndex(where: { $0.chapter == scene.chapter }) {
                    chapters[index].scenes.append(scene)
                }
            }
        }
        chapterPlans = chapters
        let known = Set(chapters.map(\.chapter))
        var groups: [(chapter: String, scenes: [Scene])] = []
        for scene in unitScenes where !known.contains(scene.chapter) {
            if let index = groups.firstIndex(where: { $0.chapter == scene.chapter }) {
                groups[index].scenes.append(scene)
            } else {
                groups.append((chapter: scene.chapter, scenes: [scene]))
            }
        }
        orphanSceneGroups = groups
    }

    /// gap_days is authoritative; gap_from_prev only when it is a bare number
    /// (legacy data wrote the day count into the prose field).
    static func gapIntervalDays(_ raw: String) -> Int? {
        let structured = scalar(raw, "gap_days")
        let bare = isBareNumber(scalar(raw, "gap_from_prev")) ? scalar(raw, "gap_from_prev") : ""
        let picked = !structured.isEmpty ? structured : bare
        guard !picked.isEmpty, let value = Double(picked), value >= 0 else { return nil }
        return Int(value)
    }

    // MARK: Scenes

    /// Reads one scene block. Scalars and lists are separate because
    /// must_land/reveal/withhold can be `[a, b]` or an indented list.
    static func parseSceneFields(_ lines: [String], fallbackChapter: String = "", index: Int = 0) -> Scene {
        func scalarValue(_ key: String) -> String {
            for line in lines {
                if let match = Rx.whole("\\s*\(Rx.escape(key))\\s*:\\s*(.*)", in: line) {
                    return unquote(match[1])
                }
            }
            return ""
        }
        func listValue(_ key: String) -> [String] {
            var out: [String] = []
            var active = false
            var base = 0
            for line in lines {
                if let match = Rx.whole("(\\s*)\(Rx.escape(key))\\s*:\\s*(.*)", in: line) {
                    active = true
                    base = match[1].count
                    let inline = match[2].trimmingCharacters(in: .whitespaces)
                    if !inline.isEmpty, inline != "[]" {
                        let body = inline.trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
                        out.append(contentsOf: body.split(separator: ",").map { unquote(String($0)) }.filter { !$0.isEmpty })
                    }
                    continue
                }
                if !active { continue }
                if line.trimmingCharacters(in: .whitespaces).isEmpty { continue }
                if let item = Rx.whole("(\\s*)-\\s+(.*)", in: line), item[1].count > base {
                    out.append(unquote(item[2]))
                    continue
                }
                active = false
            }
            return out
        }
        let whoList = listValue("who")
        let infoParts: [(String, [String])] = [
            ("must_land", listValue("must_land")),
            ("reveal", listValue("reveal")),
            ("withhold", listValue("withhold")),
        ]
        let info = infoParts.filter { !$0.1.isEmpty }.map { "\($0.0): \($0.1.joined(separator: "、"))" }.joined(separator: " · ")
        let id = scalarValue("id").isEmpty ? scalarValue("scene") : scalarValue("id")
        return Scene(
            id: id.isEmpty ? "S\(index + 1)" : id,
            chapter: scalarValue("chapter").isEmpty ? fallbackChapter : scalarValue("chapter"),
            goal: scalarValue("goal"),
            conflict: scalarValue("conflict"),
            turn: scalarValue("turn"),
            outcome: scalarValue("outcome"),
            who: whoList.isEmpty ? scalarValue("who") : whoList.joined(separator: "、"),
            place: scalarValue("where").isEmpty ? scalarValue("when") : scalarValue("where"),
            info: info
        )
    }

    /// Chapter contracts. Indentation decides what is a chapter: nested
    /// `- chapter:` lines are scenes, not new chapters (the desktop bug this
    /// mirrors: an unanchored regex duplicated every chapter).
    static func parseChapters(_ text: String) -> [ChapterPlan] {
        let body = text.replacingOccurrences(of: "\r", with: "").trimmingTrailingWhitespace()
        if body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || body.trimmingCharacters(in: .whitespacesAndNewlines) == "[]" { return [] }
        let lines = body.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        let chapterItemPattern = "(\\s*)-\\s+chapter\\s*:\\s*(\\d+)\\s*"
        var chapterIndent = -1
        for line in lines {
            if let match = Rx.whole(chapterItemPattern, in: line) { chapterIndent = match[1].count; break }
        }
        if chapterIndent < 0 { return [] }

        var chapters: [ChapterPlan] = []
        var byNumber: [String: Int] = [:]
        var current: Int?
        var sceneLines: [String] = []
        func flushScene() {
            if let current, !sceneLines.isEmpty {
                let scene = parseSceneFields(sceneLines, fallbackChapter: chapters[current].chapter, index: chapters[current].scenes.count)
                if scene.hasBody { chapters[current].scenes.append(scene) }
            }
            sceneLines = []
        }
        for line in lines {
            if line.trimmingCharacters(in: .whitespaces).isEmpty { continue }
            let indent = indentOf(line)
            let item = Rx.whole(chapterItemPattern, in: line)
            if let item, indent == chapterIndent {
                flushScene()
                let number = item[2]
                if let existing = byNumber[number] { current = existing; continue }
                chapters.append(ChapterPlan(chapter: number))
                byNumber[number] = chapters.count - 1
                current = chapters.count - 1
                continue
            }
            guard let currentIndex = current else { continue }
            if indent > chapterIndent + 2 {
                if item != nil || Rx.matches("^\\s*-\\s+(id|scene)\\s*:", in: line, multiline: false) {
                    flushScene()
                    sceneLines = [line]
                } else if !sceneLines.isEmpty {
                    sceneLines.append(line)
                }
                continue
            }
            if let field = Rx.whole("\\s*(goal|change|cut_hook|day_offset)\\s*:\\s*(.*)", in: line) {
                let value = unquote(field[2])
                switch field[1] {
                case "goal": chapters[currentIndex].goal = value
                case "change": chapters[currentIndex].change = value
                case "cut_hook": chapters[currentIndex].cutHook = value
                case "day_offset": chapters[currentIndex].dayOffset = value
                default: break
                }
            }
        }
        flushScene()
        return chapters
    }

    /// Legacy unit-level scenes: split on each new `chapter:` key or item.
    static func parseUnitScenes(_ text: String) -> [Scene] {
        let body = text.replacingOccurrences(of: "\r", with: "").trimmingTrailingWhitespace()
        if body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || body.trimmingCharacters(in: .whitespacesAndNewlines) == "[]" { return [] }
        var blocks: [[String]] = []
        var current: [String] = []
        var hasChapter = false
        for line in body.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            if line.trimmingCharacters(in: .whitespaces).isEmpty { continue }
            let chapterKey = Rx.matches("^\\s*chapter\\s*:", in: line, multiline: false)
            let itemStart = Rx.matches("^\\s*-\\s+(id|scene|chapter)\\s*:", in: line, multiline: false)
            if (chapterKey && hasChapter) || (itemStart && !current.isEmpty) {
                blocks.append(current)
                current = []
                hasChapter = false
            }
            current.append(line)
            if chapterKey { hasChapter = true }
        }
        if !current.isEmpty { blocks.append(current) }
        return blocks.enumerated().compactMap { index, block in
            let scene = parseSceneFields(block, index: index)
            guard !scene.chapter.isEmpty, scene.hasBody else { return nil }
            return scene
        }
    }
}

extension String {
    func trimmingTrailingWhitespace() -> String {
        var result = self
        while let last = result.last, last.isWhitespace || last.isNewline { result.removeLast() }
        return result
    }
}