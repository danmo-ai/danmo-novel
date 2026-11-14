import XCTest
@testable import DanmoNovelApp

final class NativeAppTests: XCTestCase {
    func testParsesUnitProseWithoutMixingChapters() {
        let chapters = Chapter.parse("""
        # 单元正文

        ## 第1章 雨夜

        第一段。

        第二段。

        ---

        ## 第 2 章 归途
        回到家。
        """)
        XCTAssertEqual(chapters.map(\.number), [1, 2])
        XCTAssertEqual(chapters.first?.title, "雨夜")
        XCTAssertEqual(chapters.first?.body, "第一段。\n\n第二段。")
        XCTAssertEqual(chapters.last?.body, "回到家。")
    }

    func testDecodesExistingBookContract() throws {
        let data = Data(#"{"id":"demo","title":"示例","genre":"悬疑","stage":"write","unitCount":1,"reviewedCount":1,"openCandidates":0,"readOnly":false}"#.utf8)
        let book = try JSONDecoder().decode(BookSummary.self, from: data)
        XCTAssertEqual(book.id, "demo")
        XCTAssertEqual(book.progress, 1)
    }

    func testEscapesBookIDAndFileQuery() {
        XCTAssertEqual("book/a?b".apiPathComponent, "book%2Fa%3Fb")
        var components = URLComponents()
        components.queryItems = [URLQueryItem(name: "path", value: "units/v01-U1.md")]
        // URLComponents permits "/" in a query value; the server decodes it as
        // the relative book path, while ".." is rejected by the handler.
        XCTAssertEqual(components.percentEncodedQuery, "path=units/v01-U1.md")
    }

    func testURLSessionReadsRealGoCoreREST() async throws {
        let home = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }
        let core = LocalCoreService(home: home)
        core.start()
        for _ in 0..<100 where !core.isReady {
            try await Task.sleep(for: .milliseconds(50))
        }
        XCTAssertTrue(core.isReady, core.error ?? "core did not start")
        let api = NovelAPIClient(core: core)
        let books = try await api.books()
        XCTAssertEqual(books.map(\.id), [])
        let created = try await api.createDemoBook()
        XCTAssertFalse(created.book.units.isEmpty)
        let booksAfter = try await api.books()
        XCTAssertEqual(booksAfter.map(\.id), [created.book.summary.id])
        let fetched = try await api.book(id: created.book.summary.id)
        let unit = try XCTUnwrap(fetched.units.first(where: \.hasProse))
        let file = try await api.file(bookID: fetched.summary.id, path: unit.prosePath)
        XCTAssertFalse(Chapter.parse(file.content).isEmpty)

        // The unit list uses the listing projection. Lock the contract: it must
        // be dramatically smaller than the full snapshot and keep unit paths.
        let id = created.book.summary.id
        let full = try await core.request(method: "GET", path: "/api/books/\(id)", body: "")
        let listing = try await core.request(method: "GET", path: "/api/books/\(id)?view=list", body: "")
        XCTAssertLessThan(listing.body.utf8.count, full.body.utf8.count / 4)
        XCTAssertFalse(listing.body.contains("\"files\""))
        XCTAssertTrue(listing.body.contains("units/"))
        core.stop()
    }
}
