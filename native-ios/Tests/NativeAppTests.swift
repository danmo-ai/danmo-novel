import XCTest
import Darwin
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

        // Slim-only contract: the default book response IS the navigation
        // skeleton — no files, no unit raws, but unit paths survive.
        let id = created.book.summary.id
        let skeleton = try await core.request(method: "GET", path: "/api/books/\(id)", body: "")
        XCTAssertFalse(skeleton.body.contains("\"files\""))
        XCTAssertFalse(skeleton.body.contains("\"proseRaw\""))
        XCTAssertTrue(skeleton.body.contains("units/"))

        // Progressive loading budget: the first screen (skeleton + slim
        // activity) must stay tiny, and the panel-document tier adds the
        // outline/bible documents without any prose.
        let panelFiles = try await core.request(method: "GET", path: "/api/books/\(id)?include=files", body: "")
        let candidates = try await core.request(method: "GET", path: "/api/books/\(id)/candidates", body: "")
        let runs = try await core.request(method: "GET", path: "/api/books/\(id)/runs?limit=50", body: "")
        let firstScreen = skeleton.body.utf8.count + candidates.body.utf8.count + runs.body.utf8.count
        XCTAssertLessThan(firstScreen, 100_000, "first screen too heavy: \(firstScreen) bytes")
        XCTAssertGreaterThan(panelFiles.body.utf8.count, skeleton.body.utf8.count,
                             "panel files must add the outline/bible documents")
        XCTAssertFalse(panelFiles.body.contains("\"proseRaw\""))
        XCTAssertFalse(candidates.body.contains("\"chapters\""))
        XCTAssertFalse(runs.body.contains("\"calls\""))
        core.stop()
    }

    /// Without a stage model the core must refuse the run with a clear error —
    /// the workspace surfaces it as an alert instead of a silent no-op.
    func testStartRunWithoutModelIsRefused() async throws {
        let home = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }
        let core = LocalCoreService(home: home)
        core.start()
        for _ in 0..<100 where !core.isReady {
            try await Task.sleep(for: .milliseconds(50))
        }
        XCTAssertTrue(core.isReady, core.error ?? "core did not start")
        defer { core.stop() }
        let api = NovelAPIClient(core: core)
        let created = try await api.createDemoBook()
        let skeleton = try await api.bookSummary(id: created.book.summary.id)
        let unit = try XCTUnwrap(skeleton.book.units.first(where: { $0.unitId == "v01-U1" }))
        var spec = TaskSpec(kind: "finalize", bookId: created.book.summary.id, baseRevision: skeleton.book.revision)
        spec.unitId = unit.unitId
        spec.volumeId = "v01"
        do {
            _ = try await api.startRun(spec)
            XCTFail("finalize without a stage model must be refused")
        } catch {
            XCTAssertTrue(error.localizedDescription.contains("model") || error.localizedDescription.contains("模型"),
                          "refusal should name the missing model: \(error.localizedDescription)")
        }
    }

    /// The whole production loop against the real Go core, no external model:
    /// a loopback endpoint answers the finalize ledger call with a valid
    /// per-chapter ledger, the run lands in waiting_accept with an open
    /// candidate (the state the progress sheet's 去任务页确认 routes to), and
    /// rejecting closes both the candidate and its run so the unit is never
    /// stranded. (The built-in mock provider cannot be scripted from a test:
    /// Go snapshots the environment at process launch, before any test code.)
    /// Not @MainActor: the mock listener's ready semaphore must not block the
    /// main queue its state handler runs on.
    func testFinalizeLoopWithMockProvider() async throws {
        // The sample's unit covers chapters 1–3; one ledger row per chapter is
        // exactly what validateUnitLedger requires.
        let ledger = #"{"chapters":[{"chapter":1,"summary":"接下委托并讲清作业规矩","stateDeltas":[],"plant":[],"payoff":[],"plannedForeshadowHandled":[]},{"chapter":2,"summary":"下水摸清船体结构","stateDeltas":[],"plant":[],"payoff":[],"plannedForeshadowHandled":[]},{"chapter":3,"summary":"气压见底前选择上浮保记录","stateDeltas":[],"plant":[],"payoff":[],"plannedForeshadowHandled":[]}]}"#
        let server = try MockLLMServer(content: ledger)
        defer { server.stop() }

        let home = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }
        let core = LocalCoreService(home: home)
        core.start()
        for _ in 0..<100 where !core.isReady {
            try await Task.sleep(for: .milliseconds(50))
        }
        XCTAssertTrue(core.isReady, core.error ?? "core did not start")
        defer { core.stop() }
        let api = NovelAPIClient(core: core)

        // An OpenAI-protocol provider pointed at the loopback mock; assign it
        // to the finalize stage.
        _ = try await api.saveProvider(ProviderRequest(
            provider: "openai", name: "uitest-mock", apiKey: "test-key",
            baseUrl: server.baseURL, defaultProtocolDialect: nil,
            models: [ProviderModel(name: "ledger")]
        ))
        _ = try await api.saveAssignments([ModelAssignment(role: "finalize", modelId: "uitest-mock/ledger")])

        let created = try await api.createDemoBook()
        let bookID = created.book.summary.id
        let skeleton = try await api.bookSummary(id: bookID)
        let unit = try XCTUnwrap(skeleton.book.units.first(where: { $0.unitId == "v01-U1" }))
        XCTAssertTrue(unit.hasProse)

        var spec = TaskSpec(kind: "finalize", bookId: bookID, baseRevision: skeleton.book.revision)
        spec.unitId = unit.unitId
        spec.volumeId = "v01"
        let run = try await api.startRun(spec)

        // Poll like the workspace does until the run terminates.
        var settled: Run?
        for _ in 0..<120 {
            let current = try await api.run(id: run.id)
            if current.isTerminated { settled = current; break }
            try await Task.sleep(for: .milliseconds(250))
        }
        let finished = try XCTUnwrap(settled, "run did not terminate")
        XCTAssertEqual(finished.status, "waiting_accept", finished.error ?? "finalize should land a candidate")
        XCTAssertGreaterThanOrEqual(server.requestCount, 1, "the finalize call must reach the model endpoint")

        // The candidate the task page would confirm is open and unit-scoped.
        let open = try await api.candidates(bookID: bookID)
        let candidate = try XCTUnwrap(open.first(where: { $0.kind == "finalize" && $0.unitId == unit.unitId }))

        // Rejecting closes the candidate and reaps the waiting run, so the
        // unit can start another task instead of being permanently occupied.
        try await api.rejectCandidate(id: candidate.id, reason: "ui loop test")
        let openAfter = try await api.candidates(bookID: bookID)
        XCTAssertFalse(openAfter.contains { $0.id == candidate.id })
        let runAfter = try await api.run(id: run.id)
        XCTAssertNotEqual(runAfter.status, "waiting_accept", "rejected candidate must not keep the run waiting")
    }
}

/// A minimal OpenAI-compatible chat endpoint on loopback, returning one fixed
/// completion. Enough for the finalize pipeline, which only reads content.
/// POSIX sockets rather than Network.framework: NWListener fails with EINVAL
/// in the unit-test host, and this server needs no entitlements.
private final class MockLLMServer: @unchecked Sendable {
    let baseURL: String
    private let serverFD: Int32
    private let content: String
    private let lock = NSLock()
    private var requests = 0
    private var running = true

    var requestCount: Int {
        lock.lock(); defer { lock.unlock() }
        return requests
    }

    init(content: String) throws {
        self.content = content
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { throw APIError.message("mock LLM socket failed") }
        var yes: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &yes, socklen_t(MemoryLayout<Int32>.size))
        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = 0 // ephemeral; read back after bind
        addr.sin_addr.s_addr = inet_addr("127.0.0.1")
        let bound = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        guard bound == 0, listen(fd, 8) == 0 else {
            close(fd)
            throw APIError.message("mock LLM bind/listen failed")
        }
        var actual = sockaddr_in()
        var len = socklen_t(MemoryLayout<sockaddr_in>.size)
        let named = withUnsafeMutablePointer(to: &actual) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(fd, $0, &len) }
        }
        guard named == 0 else {
            close(fd)
            throw APIError.message("mock LLM getsockname failed")
        }
        self.serverFD = fd
        self.baseURL = "http://127.0.0.1:\(UInt16(bigEndian: actual.sin_port))/v1"
        Thread.detachNewThread { [weak self] in self?.acceptLoop() }
    }

    func stop() {
        lock.lock(); running = false; lock.unlock()
        shutdown(serverFD, SHUT_RDWR)
        close(serverFD)
    }

    private var isRunning: Bool {
        lock.lock(); defer { lock.unlock() }
        return running
    }

    private func acceptLoop() {
        while isRunning {
            let client = accept(serverFD, nil, nil)
            if client < 0 { break }
            handle(client)
        }
    }

    private func handle(_ client: Int32) {
        defer { close(client) }
        // Read until the header terminator; the canned answer does not depend
        // on the request body, so there is no need to frame Content-Length.
        var received = Data()
        var buffer = [UInt8](repeating: 0, count: 64 * 1024)
        let terminator = Data("\r\n\r\n".utf8)
        while received.range(of: terminator) == nil {
            let n = read(client, &buffer, buffer.count)
            if n <= 0 { break }
            received.append(contentsOf: buffer[0..<n])
            if received.count > 8 * 1024 * 1024 { break }
        }
        lock.lock(); requests += 1; lock.unlock()

        let message: [String: Any] = [
            "choices": [["message": ["role": "assistant", "content": content], "finish_reason": "stop"]],
            "usage": ["prompt_tokens": 10, "completion_tokens": 10],
        ]
        let body = try! JSONSerialization.data(withJSONObject: message)
        let header = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: \(body.count)\r\nConnection: close\r\n\r\n"
        var payload = Data(header.utf8)
        payload.append(body)
        payload.withUnsafeBytes { raw in
            var sent = 0
            while sent < raw.count {
                let n = write(client, raw.baseAddress!.advanced(by: sent), raw.count - sent)
                if n <= 0 { break }
                sent += n
            }
        }
    }
}
