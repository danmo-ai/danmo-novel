import XCTest

final class NativeAppUITests: XCTestCase {
    /// Shelf -> book -> unit -> reader, then back must land on the unit list
    /// (not the shelf). This is the navigation hierarchy the phone app promises.
    @MainActor
    func testReaderBackReturnsToUnitList() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)

        let unit = app.descendants(matching: .any)["unit.row.v01-U1"]
        XCTAssertTrue(unit.waitForExistence(timeout: 20), "Unit list should load: \(app.debugDescription)")
        unit.tap()

        let chapter = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == '第 1 章'")).firstMatch
        XCTAssertTrue(chapter.waitForExistence(timeout: 15), "Reader should show prose: \(app.debugDescription)")
        XCTAssertTrue(app.buttons["阅读设置"].waitForExistence(timeout: 10))

        // System back: on Duo the bar may be vertical, so the control lives in the
        // Toolbar rather than NavigationBar, but it is still the platform's own.
        app.buttons["BackButton"].firstMatch.tap()
        XCTAssertTrue(
            app.descendants(matching: .any)["unit.row.v01-U1"].waitForExistence(timeout: 5),
            "Reader back must return to the unit list, not the shelf: \(app.debugDescription)"
        )
    }

    /// A fold/resize must not drop the reading position or the reader controls.
    @MainActor
    func testReaderSurvivesRotation() throws {
        let app = XCUIApplication()
        app.launch()
        openSampleBook(in: app)

        let unit = app.descendants(matching: .any)["unit.row.v01-U1"]
        XCTAssertTrue(unit.waitForExistence(timeout: 20))
        unit.tap()
        XCTAssertTrue(app.buttons["阅读设置"].waitForExistence(timeout: 15))

        XCUIDevice.shared.orientation = .landscapeLeft
        XCTAssertTrue(app.buttons["阅读设置"].waitForExistence(timeout: 10), "Reader controls survive landscape")
        XCUIDevice.shared.orientation = .portrait
        XCTAssertTrue(app.buttons["阅读设置"].waitForExistence(timeout: 10), "Reader controls survive rotation back")
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

    /// Uses the bundled sample so the test needs no model key. Reuses an existing
    /// copy when a previous run already created it.
    @MainActor
    private func openSampleBook(in app: XCUIApplication) {
        let sample = app.buttons["看看示例书"]
        if sample.waitForExistence(timeout: 20) {
            sample.tap()
            return
        }
        let demoBook = app.buttons["book.row.demo-fog-harbor"].firstMatch
        XCTAssertTrue(demoBook.waitForExistence(timeout: 10), "Shelf should show the sample book: \(app.debugDescription)")
        demoBook.tap()
    }
}
