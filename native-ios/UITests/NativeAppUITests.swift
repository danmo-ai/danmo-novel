import XCTest

final class NativeAppUITests: XCTestCase {
    // The immersive reader (ReaderView / ReaderSplitView, ShelfRoute.reader)
    // keeps its route and cross-pose hand-off, but the unit page no longer
    // offers a reading action — the production surface renders the prose
    // inline. The former reader-entry tests therefore assert the unit surface
    // itself: the flow menu, the inline prose, and survival across rotation
    // and the split boundary. Reader-specific behaviour (chapter paging,
    // reading settings) needs a reader entry point before it can be tested.

    /// Shelf -> book -> unit work, then back must land on the unit list
    /// (not the shelf). This is the navigation hierarchy the phone app promises.
    @MainActor
    func testUnitWorkBackReturnsToUnitList() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)

        let unit = app.descendants(matching: .any)["unit.row.v01-U1"]
        XCTAssertTrue(unit.waitForExistence(timeout: 20), "Unit list should load: \(app.debugDescription)")
        unit.tap()
        XCTAssertTrue(
            button("unit.flow.menu", in: app).waitForExistence(timeout: 15),
            "Unit tap must open the production surface: \(app.debugDescription)"
        )

        // System back: on Duo the bar may be vertical, so the control lives in the
        // Toolbar rather than NavigationBar, but it is still the platform's own.
        app.buttons["BackButton"].firstMatch.tap()
        XCTAssertTrue(
            app.descendants(matching: .any)["unit.row.v01-U1"].waitForExistence(timeout: 5),
            "Unit work back must return to the unit list, not the shelf: \(app.debugDescription)"
        )
    }

    /// The Duo split must keep the unit's prose reachable, and revealing the
    /// bookshelf (which narrows the detail column) must not drop it.
    @MainActor
    func testUnitWorkProseReachableInSplit() throws {
        let app = XCUIApplication()
        app.launch()
        XCUIDevice.shared.orientation = .landscapeLeft
        defer { XCUIDevice.shared.orientation = .portrait }
        Thread.sleep(forTimeInterval: 3)

        openSampleBook(in: app)
        let unit = app.descendants(matching: .any)["unit.row.v01-U1"]
        XCTAssertTrue(unit.waitForExistence(timeout: 20), "Unit list should load: \(app.debugDescription)")
        unit.tap()
        XCTAssertTrue(button("unit.flow.menu", in: app).waitForExistence(timeout: 15))

        let prose = app.staticTexts.matching(
            NSPredicate(format: "label CONTAINS '雾从防波堤'")).firstMatch
        XCTAssertTrue(prose.waitForExistence(timeout: 20), "Unit work must render the prose inline: \(app.debugDescription)")

        // Reveal the bookshelf, which narrows the detail column.
        let toggle = app.navigationBars.buttons.matching(
            NSPredicate(format: "label CONTAINS 'Sidebar'")).firstMatch
        if toggle.waitForExistence(timeout: 5) { toggle.tap() }
        Thread.sleep(forTimeInterval: 3)

        XCTAssertTrue(prose.exists, "Prose must stay reachable when the sidebar is revealed: \(app.debugDescription)")
        XCTAssertTrue(button("unit.flow.menu", in: app).exists, "Flow menu must survive the narrow detail column")
    }

    /// A fold/resize must not drop the unit surface or its flow menu.
    @MainActor
    func testUnitWorkSurvivesRotation() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)

        let unit = app.descendants(matching: .any)["unit.row.v01-U1"]
        XCTAssertTrue(unit.waitForExistence(timeout: 20))
        unit.tap()
        XCTAssertTrue(button("unit.flow.menu", in: app).waitForExistence(timeout: 15))

        XCUIDevice.shared.orientation = .landscapeLeft
        XCTAssertTrue(button("unit.flow.menu", in: app).waitForExistence(timeout: 10), "Flow menu survives landscape")
        XCUIDevice.shared.orientation = .portrait
        XCTAssertTrue(button("unit.flow.menu", in: app).waitForExistence(timeout: 10), "Flow menu survives rotation back: \(app.debugDescription)")
    }

    @MainActor
    func testNewBookUsesNativeSheet() throws {
        let app = XCUIApplication()
        app.launch()
        let add = app.buttons["新建书籍"]
        XCTAssertTrue(add.waitForExistence(timeout: 20))
        add.tap()
        XCTAssertTrue(app.navigationBars["新建书籍"].waitForExistence(timeout: 5))
        if app.buttons["取消"].exists {
            app.buttons["取消"].tap()
        }
        XCTAssertTrue(app.navigationBars["我的书架"].waitForExistence(timeout: 5))
    }

    /// The book workspace exposes four panels and the unit production flow.
    /// Guards the port: tabs render, unit work shows the four-step actions, and
    /// the outline/bible/task panels load.
    @MainActor
    func testBookWorkspacePanelsAndUnitFlow() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)

        // Primary next-step banner and the four tabs.
        XCTAssertTrue(app.buttons["book.primary"].waitForExistence(timeout: 20))
        for tab in ["写作", "大纲", "设定", "任务"] {
            XCTAssertTrue(app.buttons[tab].waitForExistence(timeout: 5), "Tab \(tab) should exist")
        }

        // Unit work: the four steps are one system toolbar Menu (单元操作);
        // there is no separate reading action — the prose renders inline.
        let unit = app.descendants(matching: .any)["unit.row.v01-U1"]
        XCTAssertTrue(unit.waitForExistence(timeout: 10))
        unit.tap()
        openFlowMenu(in: app)
        XCTAssertTrue(flowAction("outline", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(flowAction("write", in: app).exists)
        XCTAssertTrue(flowAction("review", in: app).exists)
        XCTAssertTrue(flowAction("finalize", in: app).exists)
        XCTAssertFalse(app.buttons["阅读正文"].exists, "Unit page no longer offers a reading action")
        dismissFlowMenu(in: app)
        app.buttons["BackButton"].firstMatch.tap()

        // Outline panel renders the structured book outline.
        app.buttons["大纲"].tap()
        XCTAssertTrue(app.staticTexts["全书总纲"].waitForExistence(timeout: 10)
            || app.staticTexts.matching(NSPredicate(format: "label CONTAINS '总纲'")).firstMatch.waitForExistence(timeout: 5))

        // Bible panel lists the derived settings docs.
        app.buttons["设定"].tap()
        XCTAssertTrue(app.staticTexts["作者意图与读者承诺"].waitForExistence(timeout: 10))

        // Task page: the single confirmation surface (inbox fused with the log).
        app.buttons["任务"].tap()
        XCTAssertTrue(
            app.staticTexts["还没有任务"].waitForExistence(timeout: 15)
                || app.staticTexts["当前没有待确认的结果。"].exists
                || app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '操作记录'")).firstMatch.exists,
            "Task page must render its inbox or empty state: \(app.debugDescription)"
        )
    }

    /// The outline panel is a system List with a document-selection Menu and a
    /// system actions Menu — not hand-built chips. The structured book outline
    /// renders, the unit index expands through the system DisclosureGroup, and
    /// the volume document is reachable from the picker.
    @MainActor
    func testOutlinePanelStructureAndNavigation() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)
        app.buttons["大纲"].tap()

        // Document picker + actions are system menus.
        let picker = button("outline.document.menu", in: app)
        XCTAssertTrue(picker.waitForExistence(timeout: 15), "Outline document picker should exist: \(app.debugDescription)")
        XCTAssertTrue(button("outline.actions.menu", in: app).waitForExistence(timeout: 10))

        // The structured book outline renders its title.
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS '总纲'")).firstMatch.waitForExistence(timeout: 10))

        // The actions menu offers raw editing and structure review.
        button("outline.actions.menu", in: app).tap()
        XCTAssertTrue(app.buttons["原始 MD"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["结构审校"].exists)
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.1)).tap() // dismiss menu

        // Switch to the volume document through the system picker; the unit
        // seed index lives in the volume outline, not the book outline.
        picker.tap()
        let volumeOption = app.buttons["第 1 卷"]
        XCTAssertTrue(volumeOption.waitForExistence(timeout: 5), "Volume option should be in the picker")
        volumeOption.tap()

        // The seed index can start below the fold (notably on the Duo's split
        // detail); scroll the outline list until it appears.
        let seed = app.descendants(matching: .any)["outline.seed.v01-U1"]
        var scrolls = 0
        while !seed.exists, scrolls < 12 {
            app.swipeUp()
            scrolls += 1
        }
        XCTAssertTrue(seed.exists, "Unit seed index should render in the volume outline: \(app.debugDescription)")
    }

    /// Raw-MD editing opens a form sheet with system cancel/save and asks
    /// before discarding a dirty draft — not an inline editor.
    @MainActor
    func testOutlineRawEditorSheet() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)
        app.buttons["大纲"].tap()
        XCTAssertTrue(button("outline.actions.menu", in: app).waitForExistence(timeout: 15))
        button("outline.actions.menu", in: app).tap()
        let raw = app.buttons["原始 MD"]
        XCTAssertTrue(raw.waitForExistence(timeout: 5))
        raw.tap()

        // The sheet opens with the save action; a clean cancel closes it.
        XCTAssertTrue(app.buttons["保存 MD"].waitForExistence(timeout: 10), "Raw editor sheet should open: \(app.debugDescription)")
        app.buttons["取消"].tap()
        XCTAssertTrue(button("outline.document.menu", in: app).waitForExistence(timeout: 10), "Clean cancel returns to the outline")
    }

    /// The task page is where every result is confirmed. It must render its
    /// sections (or the empty state) and expose the operation log — the review
    /// and candidate tabs it replaced are gone.
    @MainActor
    func testTaskPageIsTheConfirmationSurface() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)

        app.buttons["任务"].tap()
        // Either pending work or the explicit empty state, never a blank pane.
        let empty = app.staticTexts["还没有任务"]
        let nothingPending = app.staticTexts["当前没有待确认的结果。"]
        let history = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '操作记录'")).firstMatch
        XCTAssertTrue(
            empty.waitForExistence(timeout: 15) || nothingPending.exists || history.exists,
            "Task page must show its inbox or empty state: \(app.debugDescription)"
        )
        // The replaced tabs must not linger.
        XCTAssertFalse(app.buttons["审校"].exists, "Review tab was merged into the task page")
        XCTAssertFalse(app.buttons["候选"].exists, "Candidate tab was merged into the task page")
    }

    /// Chapter actions are a system menu (plus swipe/context menu), not bare
    /// caption buttons: the ellipsis opens 复制本章 / 编辑本章, and the editor
    /// sheet asks before discarding a dirty draft.
    @MainActor
    func testChapterMenuAndEditorSheet() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)
        openUnitWork(in: app, unitID: "v01-U1")

        let menu = button("unit.chapter.menu.1", in: app)
        XCTAssertTrue(menu.waitForExistence(timeout: 15), "Chapter actions menu should exist: \(app.debugDescription)")
        menu.tap()
        XCTAssertTrue(app.buttons["复制本章"].waitForExistence(timeout: 5))
        let edit = app.buttons["编辑本章"]
        XCTAssertTrue(edit.exists, "Chapter menu should offer editing")
        edit.tap()

        // The editor sheet opens with the chapter title and system cancel/save.
        let editor = app.textViews["unit.chapter.editor"]
        XCTAssertTrue(editor.waitForExistence(timeout: 10), "Editor sheet must open")

        // Dirty the draft so cancel must ask before discarding. Re-query right
        // before typing: a stale element handle fails to resolve its frame.
        let freshEditor = app.textViews["unit.chapter.editor"]
        freshEditor.coordinate(withNormalizedOffset: CGVector(dx: 0.2, dy: 0.1)).tap()
        if app.keyboards.firstMatch.waitForExistence(timeout: 5) {
            app.typeText("改")
            app.buttons["取消"].tap()
            XCTAssertTrue(app.buttons["放弃修改"].waitForExistence(timeout: 5), "Dirty cancel must confirm discard")
            // Keep editing: dismiss the confirmation by tapping outside it (the
            // cancel-role button is not exposed as a tappable element). The
            // editor must still be there with the save action.
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.15)).tap()
            XCTAssertTrue(app.buttons["保存"].waitForExistence(timeout: 5), "Continuing keeps the editor open")
            // Discard for real this time; the unit surface comes back untouched.
            app.buttons["取消"].tap()
            XCTAssertTrue(app.buttons["放弃修改"].waitForExistence(timeout: 5))
            app.buttons["放弃修改"].tap()
        } else {
            // Some simulators (the Duo) never give the sheet's TextEditor
            // keyboard focus, so the draft cannot be dirtied here. The clean
            // cancel path still proves the sheet closes back to the unit.
            app.buttons["取消"].tap()
        }
        XCTAssertTrue(button("unit.flow.menu", in: app).waitForExistence(timeout: 10))
    }

    /// The production loop's entry without a model key: the flow menu's finalize
    /// step opens the confirm sheet, the sheet spells out the stage model, and
    /// confirming surfaces the core's refusal as a dismissible alert — never a
    /// silent no-op. The full waiting_accept → task-page loop runs against the
    /// mock provider in NativeAppTests (a UI test cannot seed the app sandbox).
    @MainActor
    func testFinalizeConfirmAndNoModelGuard() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)
        openUnitWork(in: app, unitID: "v01-U1")

        openFlowMenu(in: app)
        let finalize = flowAction("finalize", in: app)
        XCTAssertTrue(finalize.waitForExistence(timeout: 15))
        finalize.tap()

        // The confirm sheet opens with the finalize verb and the stage model.
        // LabeledContent merges label + value into one accessibility element.
        let confirm = app.buttons["确认账本核对"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 10), "Finalize confirm sheet should open: \(app.debugDescription)")
        let stageModel = app.staticTexts["模型, 按阶段分配"]
        XCTAssertTrue(stageModel.waitForExistence(timeout: 10), "Stage model row missing: \(app.debugDescription)")
        confirm.tap()

        // No model is assigned on a fresh install: the core refuses the run and
        // the app must say so, then return to the unit surface on dismiss.
        let alert = app.alerts.firstMatch
        if !alert.waitForExistence(timeout: 20) {
            let shot = XCUIScreen.main.screenshot()
            let attachment = XCTAttachment(screenshot: shot)
            attachment.name = "no-alert-state"
            attachment.lifetime = .keepAlways
            add(attachment)
            XCTFail("Run refusal must surface as an alert. Sheet still up: \(confirm.exists), alerts: \(app.alerts.count), texts: \(app.staticTexts.allElementsBoundByIndex.map { $0.label }.prefix(12))")
        }
        alert.buttons["好"].tap()
        XCTAssertTrue(button("unit.flow.menu", in: app).waitForExistence(timeout: 5))
    }

    /// The knowledge tab lists articles and opens one for reading.
    @MainActor
    func testKnowledgeTabOpensArticle() throws {
        let app = XCUIApplication()
        app.launch()
        tapTab("知识", in: app)
        let row = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'knowledge.row.'")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 20), "Knowledge list should load: \(app.debugDescription)")
        row.tap()
        XCTAssertTrue(app.buttons["编辑"].waitForExistence(timeout: 10))
    }

    /// Settings exposes the stage assignments and review parameters ported
    /// from the desktop.
    @MainActor
    func testSettingsShowsStagesAndReview() throws {
        let app = XCUIApplication()
        app.launch()
        tapTab("设置", in: app)
        let stages = app.staticTexts["创作阶段模型"]
        XCTAssertTrue(stages.waitForExistence(timeout: 20))
        // The grouped form is long; the later sections are below the fold.
        // Scroll the settings list itself — an app-level swipe can miss it in
        // the Duo's split layout.
        let list = app.collectionViews.firstMatch.exists ? app.collectionViews.firstMatch : app.tables.firstMatch
        let review = app.staticTexts["审校设置"]
        var scrolls = 0
        while !review.exists, scrolls < 10 {
            if list.exists { list.swipeUp() } else { app.swipeUp() }
            scrolls += 1
        }
        XCTAssertTrue(review.exists, "Review settings section should be reachable")
        let modelConfig = app.staticTexts["模型参数"]
        scrolls = 0
        while !modelConfig.exists, scrolls < 10 {
            if list.exists { list.swipeUp() } else { app.swipeUp() }
            scrolls += 1
        }
        XCTAssertTrue(modelConfig.exists, "Model config section should be reachable")
    }

    /// Taps a top-level tab in either pose: a horizontal tab bar on compact
    /// width, a vertical rail on the Duo (where `tabBars` does not match).
    @MainActor
    private func tapTab(_ label: String, in app: XCUIApplication) {
        let tabBarButton = app.tabBars.buttons[label]
        if tabBarButton.waitForExistence(timeout: 3) {
            tabBarButton.tap()
            return
        }
        let railButton = app.buttons[label]
        XCTAssertTrue(railButton.waitForExistence(timeout: 10), "Tab \(label) should exist: \(app.debugDescription)")
        railButton.tap()
    }

    /// The four production steps are one system toolbar Menu (单元操作). A step
    /// is only tappable once the menu is open, so every flow-action assertion
    /// opens the group first and dismisses it when it only checked existence.
    @MainActor
    private func openFlowMenu(in app: XCUIApplication) {
        let menu = button("unit.flow.menu", in: app)
        XCTAssertTrue(menu.waitForExistence(timeout: 15), "Unit flow menu should exist: \(app.debugDescription)")
        menu.tap()
    }

    /// Dismisses an open flow menu by tapping the bar area above the content.
    @MainActor
    private func dismissFlowMenu(in app: XCUIApplication) {
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.05)).tap()
    }

    /// A flow step inside the opened 单元操作 menu.
    @MainActor
    private func flowAction(_ kind: String, in app: XCUIApplication) -> XCUIElement {
        button("unit.action.\(kind)", in: app)
    }

    /// Opens a unit's production surface from the book's write tab.
    @MainActor
    private func openUnitWork(in app: XCUIApplication, unitID: String) {
        let unit = app.descendants(matching: .any)["unit.row.\(unitID)"]
        XCTAssertTrue(unit.waitForExistence(timeout: 20), "Unit list should load: \(app.debugDescription)")
        unit.tap()
    }

    /// Toolbar items can live outside `buttons` on the Duo's vertical rail, so
    /// fall back to a descendant query by identifier — the same reason
    /// `tapTab` does not trust `tabBars` alone.
    @MainActor
    private func button(_ identifier: String, in app: XCUIApplication) -> XCUIElement {
        let direct = app.buttons[identifier]
        if direct.exists { return direct }
        return app.descendants(matching: .any)[identifier]
    }

    /// Uses the bundled sample so the test needs no model key. Reuses an existing
    /// copy when a previous run already created it.
    @MainActor
    private func openSampleBook(in app: XCUIApplication) {
        let sample = app.buttons["看看示例书"]
        if sample.waitForExistence(timeout: 20) {
            sample.tap()
            return
        }
        // A sidebar selection row is exposed as a cell, not a button, so query by
        // identifier rather than element type.
        let demoBook = app.descendants(matching: .any)["book.row.demo-fog-harbor"].firstMatch
        XCTAssertTrue(demoBook.waitForExistence(timeout: 10), "Shelf should show the sample book: \(app.debugDescription)")
        demoBook.tap()
    }
}
