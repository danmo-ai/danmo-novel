import XCTest
@testable import DanmoNovelApp

/// The ported desktop logic must behave like its TypeScript original; these
/// lock the rules that the workspace's correctness depends on.
final class WorkspaceLogicTests: XCTestCase {
    // MARK: Review verdict (must match domain.ReviewVerdict)

    private func finding(_ id: String, severity: String, chapter: Int? = 1, roundsHit: Int? = nil) -> ReviewFinding {
        ReviewFinding(id: id, severity: severity, source: "ai_review", lens: nil, title: "t",
                      evidence: nil, anchor: nil, roundsHit: roundsHit, chapter: chapter,
                      suggestion: nil, entityPath: nil, volumeId: nil, unitId: nil)
    }

    func testReviewVerdictMatchesBackendRules() {
        XCTAssertEqual(ReviewState.verdict([]), .clean)
        // Single-round blocking does not block (unstable sampling).
        XCTAssertEqual(ReviewState.verdict([finding("a", severity: "blocking", roundsHit: 1)]), .clean)
        // Two independent rounds do.
        XCTAssertEqual(ReviewState.verdict([finding("a", severity: "blocking", roundsHit: 2)]), .blocking)
        // Legacy/local-gate findings without roundsHit keep blocking semantics.
        XCTAssertEqual(ReviewState.verdict([finding("a", severity: "blocking")]), .blocking)
        // Truncation markers are never "clean".
        XCTAssertEqual(ReviewState.verdict([finding("review-truncated", severity: "advisory")]), .unfinished)
        // Blocking wins over unfinished.
        XCTAssertEqual(ReviewState.verdict([
            finding("review-incomplete", severity: "advisory"),
            finding("b", severity: "blocking", roundsHit: 3),
        ]), .blocking)
    }

    func testProseFixableRequiresChapter() {
        XCTAssertFalse(ReviewState.isProseFixable(finding("stale-x", severity: "blocking", chapter: nil)))
        // The unfinished marker is excluded by canPick, not by isProseFixable
        // (the desktop keeps the same split).
        let truncated = ReviewRow(finding: finding("review-truncated", severity: "advisory"), candidateId: "c", unitId: "u")
        XCTAssertFalse(truncated.canPick)
        XCTAssertTrue(ReviewState.isProseFixable(finding("continuity-51-1", severity: "blocking", chapter: 51)))
    }

    // MARK: Knowledge filtering

    func testGenreFamiliesAndAliases() {
        XCTAssertEqual(KnowledgeFilter.genreFamilies("悬疑灵异"), ["悬疑"])
        XCTAssertEqual(KnowledgeFilter.genreFamilies("言情"), ["现代言情", "古代言情"])
        XCTAssertEqual(KnowledgeFilter.genreFamilies("凡人修仙传"), ["仙侠"])
        XCTAssertEqual(KnowledgeFilter.canonicalSubgenre("年代刑侦"), "刑侦探案")
    }

    func testKnowledgeApplicability() {
        let doc = KnowledgeDoc(id: "x", title: "t", summary: "", bytes: 1, category: "genre",
                               builtin: true, customized: false, stages: ["write"],
                               genres: ["悬疑"], subgenres: ["刑侦"], isDefault: false, audience: nil)
        XCTAssertTrue(KnowledgeFilter.appliesTo(stages: doc.stages, stage: "write"))
        XCTAssertFalse(KnowledgeFilter.appliesTo(stages: doc.stages, stage: "review"))
        XCTAssertTrue(KnowledgeFilter.appliesToGenre(doc, bookGenre: "悬疑灵异", bookSubgenre: "年代刑侦"))
        XCTAssertFalse(KnowledgeFilter.appliesToGenre(doc, bookGenre: "玄幻", bookSubgenre: nil))
        // Subgenre restriction without a book subgenre fails.
        XCTAssertFalse(KnowledgeFilter.appliesToGenre(doc, bookGenre: "悬疑", bookSubgenre: ""))
        // Untagged = everywhere.
        XCTAssertTrue(KnowledgeFilter.appliesTo(stages: nil, stage: "write"))
    }

    // MARK: Sentence diff

    func testSentenceDiffHighlightsOnlyChangedCharacters() {
        let rows = TextDiff.chapterRows(
            base: [ChapterDraft(chapter: 1, title: "t", body: "他把杯子放下，说：「七天了。」")],
            revised: [ChapterDraft(chapter: 1, title: "t", body: "他把杯子放下，说：「五年了。」")]
        )
        XCTAssertEqual(rows.count, 1)
        XCTAssertTrue(rows[0].changed)
        let segments = rows[0].segments
        // The unchanged prefix must survive as a same segment…
        XCTAssertTrue(segments.contains { $0.kind == .same && $0.text.contains("他把杯子放下") })
        // …and the refinement must not repeat the whole sentence.
        let added = segments.filter { $0.kind == .add }.map(\.text).joined()
        let removed = segments.filter { $0.kind == .del }.map(\.text).joined()
        XCTAssertTrue(added.contains("五"))
        XCTAssertTrue(removed.contains("七"))
        XCTAssertLessThan(added.count, 10)
    }

    func testUnchangedChapterIsNotChanged() {
        let rows = TextDiff.chapterRows(
            base: [ChapterDraft(chapter: 2, title: "t", body: "原样。")],
            revised: [ChapterDraft(chapter: 2, title: "t", body: "原样。")]
        )
        XCTAssertFalse(rows[0].changed)
    }

    // MARK: Structured outline fallback parser

    func testParsesStructuredOutlineEntities() {
        let raw = """
        # 全书总纲 · 雾港
        - schema_version: 1
        - logline: 一句话

        ## 故事线 · main
        - description: 主线
        - opening_state: 起点
        - ending_state: 终点

        ## 单元 · v01-U1
        - title: 雾起
        - objective: 目标
        - outcome: 结果
        """
        let doc = StructuredMarkdownParser.parse(raw, protocolName: "book-outline/v1")
        XCTAssertFalse(doc.legacy)
        XCTAssertEqual(doc.title, "全书总纲 · 雾港")
        XCTAssertEqual(doc.root.value("logline"), "一句话")
        XCTAssertEqual(doc.entities.count, 2)
        XCTAssertEqual(doc.entities[1].kind, "单元")
        XCTAssertEqual(doc.entities[1].value("outcome"), "结果")
        XCTAssertTrue(StructuredMarkdownParser.parse("# 旧格式", protocolName: "book-outline/v1").legacy)
    }

    // MARK: Unit contract

    func testParsesUnitContractChaptersAndScenes() {
        let yaml = """
        unit_id: v01-U1
        function: 获得入口
        desire: 查案
        obstacle: 规矩
        choice: 申请
        payoff: 许可
        pov: wen-nianci
        on_stage: [wen-nianci, pei-guo-dong]
        story_day: "91"
        gap_days: 1
        chapters:
          - chapter: 51
            goal: 获得复查入口
            change: 批准三天复查
            cut_hook: 签名反常
            day_offset: 0
            scenes:
              - id: S1
                chapter: 51
                goal: 争取复查
                conflict: 疑点不足
                turn: 申请重勘
                outcome: 三天许可
                must_land: [明确三天期限]
          - chapter: 52
            goal: 现场重勘
            change: 发现新线索
            cut_hook: 门外有人
            day_offset: 1
        """
        let unit = UnitInfo(unitId: "v01-U1", volume: 1, index: 1, status: "accepted", title: "只谈案子",
                            chapterFrom: 51, chapterTo: 52, hasProse: false, outlinePath: "outline/units/v01-U1.yaml",
                            prosePath: "units/v01-U1.md", chapterTarget: 2500, outlineRaw: yaml, proseRaw: nil)
        let contract = UnitContract(unit: unit, book: nil)
        XCTAssertTrue(contract.hasContent)
        XCTAssertEqual(contract.chapterPlans.count, 2)
        XCTAssertEqual(contract.chapterPlans[0].goal, "获得复查入口")
        XCTAssertEqual(contract.chapterPlans[0].scenes.count, 1)
        XCTAssertEqual(contract.chapterPlans[0].scenes[0].outcome, "三天许可")
        XCTAssertTrue(contract.chapterPlans[0].scenes[0].info.contains("must_land"))
        XCTAssertEqual(contract.chapterPlans[1].scenes.count, 0)
        XCTAssertEqual(contract.onStage, ["wen-nianci", "pei-guo-dong"])
        XCTAssertTrue(contract.meta.contains { $0.contains("2,500") || $0.contains("2500") })
        XCTAssertTrue(contract.meta.contains(Copy.contractGapDays(1)))
    }

    func testNestedChapterLinesAreScenesNotChapters() {
        // A nested `- chapter:` inside scenes must not create a new chapter.
        let yaml = """
        chapters:
          - chapter: 51
            goal: g
            scenes:
              - chapter: 51
                goal: sg
                conflict: sc
                turn: st
                outcome: so
        """
        let plans = UnitContract.parseChapters(UnitContract.section(yaml, "chapters"))
        XCTAssertEqual(plans.count, 1)
        XCTAssertEqual(plans[0].scenes.count, 1)
    }

    // MARK: Chapter replacement (manual prose edit)

    func testReplaceChapterKeepsOtherChaptersByteIdentical() {
        let original = "## 第1章 雨夜\n\n第一段。\n\n---\n\n## 第2章 归途\n\n回到家。\n"
        let edited = "## 第1章 雨夜\n\n改过的第一段。"
        let updated = Chapter.replaceChapter(1, in: original, with: edited)
        XCTAssertNotNil(updated)
        XCTAssertTrue(updated!.contains("改过的第一段。"))
        XCTAssertTrue(updated!.contains("## 第2章 归途\n\n回到家。"))
        XCTAssertNil(Chapter.replaceChapter(9, in: original, with: edited))
    }

    // MARK: Cast relations

    func testParsesFormalAndAnchorRelations() {
        let protagonist = CastCard(stem: "wen-nianci", name: "温念慈", tagline: nil, status: "canon", role: "protagonist",
                                   raw: "# 温念慈\n\n## 关系\n\n| 对方 | 类型 | 当前 |\n| --- | --- | --- |\n| pei-guo-dong | 上下级 | 试用观察 |\n")
        let other = CastCard(stem: "pei-guo-dong", name: "裴国栋", tagline: nil, status: "canon", role: "recurring",
                             raw: "# 裴国栋\n\n关系锚点：温念慈（旧案心结）；林晚（旧识）\n与主角关系：带教与试探\n")
        let third = CastCard(stem: "lin-wan", name: "林晚", tagline: nil, status: "canon", role: "recurring", raw: "# 林晚\n")
        let relations = CastRelations.parse(cards: [protagonist, other, third])
        XCTAssertTrue(relations.contains { $0.formal && $0.from == "wen-nianci" && $0.to == "pei-guo-dong" })
        XCTAssertTrue(relations.contains { !$0.formal && $0.to == "lin-wan" && $0.current.contains("旧识") })
        // The unordered pair is the dedup key (desktop semantics): the formal
        // table already claimed wen↔pei, so the anchor/与主角 lines for that
        // same pair do not add a second edge.
        XCTAssertEqual(relations.filter { $0.id == ["wen-nianci", "pei-guo-dong"].sorted().joined(separator: "|") }.count, 1)
        XCTAssertEqual(Set(relations.map(\.id)).count, relations.count)
    }

    // MARK: Identity at unit

    func testIdentitySegmentInForceAtUnit() throws {
        let json = """
        {"summary":{"id":"b","title":"t","genre":"悬疑","stage":"writing","unitCount":2,"reviewedCount":0,"openCandidates":0,"readOnly":false},
         "volumes":[],"units":[],"cast":[],"revision":"r",
         "identitySegments":[
           {"stem":"wen","roleTitle":"内勤新警","fromUnit":"v01-U1","toUnit":"v01-U3"},
           {"stem":"wen","roleTitle":"重案队试用","fromUnit":"v02-U1"}
         ]}
        """
        let book = try JSONDecoder().decode(BookSnapshot.self, from: Data(json.utf8))
        XCTAssertEqual(book.identityAt(stem: "wen", unitID: "v01-U2"), "内勤新警")
        XCTAssertEqual(book.identityAt(stem: "wen", unitID: "v02-U1"), "重案队试用")
        XCTAssertEqual(book.identityAt(stem: "wen", unitID: "v01-U4"), "")
    }

    // MARK: New book slug

    func testSlugifyMatchesDesktopRule() {
        XCTAssertEqual(NewBookView.slugify("My Novel!"), "my-novel")
        XCTAssertTrue(NewBookView.slugify("雾港").hasPrefix("book-"))
        XCTAssertEqual(NewBookView.slugify(""), "")
    }
}