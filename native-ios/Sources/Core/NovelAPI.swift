import Foundation

/// URLSession still supplies the REST request/response semantics. Only the
/// transport is replaced: danmo://local is fulfilled by the in-process Go
/// handler rather than a TCP listener, keeping the API private to this app.
final class LocalCoreURLProtocol: URLProtocol, @unchecked Sendable {
    var core: LocalCoreService?

    override class func canInit(with request: URLRequest) -> Bool {
        request.url?.scheme == "danmo" && request.url?.host == "local"
    }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override init(request: URLRequest, cachedResponse: CachedURLResponse?, client: (any URLProtocolClient)?) {
        super.init(request: request, cachedResponse: cachedResponse, client: client)
        core = URLProtocol.property(forKey: "LocalCore", in: request) as? LocalCoreService
    }

    override func startLoading() {
        guard let core else {
            client?.urlProtocol(self, didFailWithError: APIError.coreNotReady)
            return
        }
        guard let url = request.url else {
            client?.urlProtocol(self, didFailWithError: APIError.message(Copy.apiInvalidURL))
            return
        }
        var path = url.path
        if let query = url.query { path += "?" + query }
        // URLSession hands custom protocols the body as a stream, not as
        // httpBody: reading only httpBody silently dropped every POST/PUT
        // payload (the core saw an empty body and gin reported EOF).
        let body = Self.bodyString(of: request)
        Task {
            do {
                let response = try await core.request(method: request.httpMethod ?? "GET", path: path, body: body)
                let httpResponse = HTTPURLResponse(url: url, statusCode: response.status, httpVersion: nil, headerFields: ["Content-Type": "application/json; charset=utf-8"])!
                client?.urlProtocol(self, didReceive: httpResponse, cacheStoragePolicy: .notAllowed)
                client?.urlProtocol(self, didLoad: Data(response.body.utf8))
                client?.urlProtocolDidFinishLoading(self)
            } catch { client?.urlProtocol(self, didFailWithError: error) }
        }
    }
    override func stopLoading() {}

    /// The request body, whether URLSession kept it as data or converted it
    /// to a stream (it does the latter before custom protocols see it).
    static func bodyString(of request: URLRequest) -> String {
        if let data = request.httpBody, !data.isEmpty {
            return String(decoding: data, as: UTF8.self)
        }
        guard let stream = request.httpBodyStream else { return "" }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let bufferSize = 64 * 1024
        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: bufferSize)
        defer { buffer.deallocate() }
        while stream.hasBytesAvailable {
            let read = stream.read(buffer, maxLength: bufferSize)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return String(decoding: data, as: UTF8.self)
    }
}

struct NovelAPIClient: Sendable {
    private let core: LocalCoreService
    private let session: URLSession
    private let decoder = JSONDecoder()
    private let encoder = JSONEncoder()

    init(core: LocalCoreService) {
        self.core = core
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [LocalCoreURLProtocol.self]
        session = URLSession(configuration: configuration)
        // Runs can take minutes; the preview and generation requests are all
        // in-process so a long timeout costs nothing.
        configuration.timeoutIntervalForRequest = 600
    }

    func get<T: Decodable>(_ type: T.Type, path: String) async throws -> T {
        try await perform(type, method: "GET", path: path)
    }

    func post<T: Decodable, Body: Encodable>(_ type: T.Type, path: String, body: Body) async throws -> T {
        let data = try encoder.encode(body)
        return try await perform(type, method: "POST", path: path, body: String(decoding: data, as: UTF8.self))
    }

    func put<T: Decodable, Body: Encodable>(_ type: T.Type, path: String, body: Body) async throws -> T {
        let data = try encoder.encode(body)
        return try await perform(type, method: "PUT", path: path, body: String(decoding: data, as: UTF8.self))
    }

    func patch<T: Decodable, Body: Encodable>(_ type: T.Type, path: String, body: Body) async throws -> T {
        let data = try encoder.encode(body)
        return try await perform(type, method: "PATCH", path: path, body: String(decoding: data, as: UTF8.self))
    }

    func delete<T: Decodable>(_ type: T.Type, path: String) async throws -> T {
        try await perform(type, method: "DELETE", path: path)
    }

    /// POST without a request body (endpoints that bind nothing).
    func post<T: Decodable>(_ type: T.Type, path: String) async throws -> T {
        try await perform(type, method: "POST", path: path)
    }

    private func perform<T: Decodable>(_ type: T.Type, method: String, path: String, body: String = "") async throws -> T {
        guard let url = URL(string: "danmo://local\(path)") else { throw APIError.message(Copy.apiInvalidPath) }
        let mutableRequest = NSMutableURLRequest(url: url)
        URLProtocol.setProperty(core, forKey: "LocalCore", in: mutableRequest)
        var urlRequest = mutableRequest as URLRequest
        urlRequest.httpMethod = method
        if !body.isEmpty {
            urlRequest.httpBody = Data(body.utf8)
            urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, rawResponse) = try await session.data(for: urlRequest)
        let response = rawResponse as? HTTPURLResponse
        guard let status = response?.statusCode, (200..<300).contains(status) else {
            if let error = try? decoder.decode(APIErrorBody.self, from: data) {
                throw APIError.message(error.error)
            }
            throw APIError.http(response?.statusCode ?? -1)
        }
        do { return try decoder.decode(type, from: data) }
        catch { throw APIError.decoding(error.localizedDescription) }
    }
}

enum APIError: LocalizedError {
    case coreNotReady
    case http(Int)
    case message(String)
    case decoding(String)

    var errorDescription: String? {
        switch self {
        case .coreNotReady: Copy.apiCoreNotReady
        case .http(let status): Copy.apiHTTPError(status)
        case .message(let message), .decoding(let message): message
        }
    }
}

struct APIErrorBody: Decodable { let error: String }

// MARK: - Books

extension NovelAPIClient {
    func books() async throws -> [BookSummary] {
        try await get([BookSummary].self, path: "/api/books")
    }

    func book(id: String) async throws -> BookDetail {
        // The book endpoint is slim-only: summary + unit metadata, no raw text.
        try await get(BookListingResponse.self, path: "/api/books/\(id.apiPathComponent)").book
    }

    /// Navigation skeleton: summary + unit metadata + primary + continuity,
    /// no raw text. The first screen loads this; panel documents arrive later
    /// through bookPanelFiles.
    func bookSummary(id: String) async throws -> BookResponse {
        try await get(BookResponse.self, path: "/api/books/\(id.apiPathComponent)")
    }

    /// Skeleton plus exactly the documents the outline/bible panels render
    /// (outlines, canon, bible, run state, continuity). Unit raws stay out.
    func bookPanelFiles(id: String) async throws -> BookResponse {
        try await get(BookResponse.self, path: "/api/books/\(id.apiPathComponent)?include=files")
    }

    func createBook(_ draft: NewBookDraft) async throws -> BookDetail {
        try await post(BookDetail.self, path: "/api/books", body: draft)
    }

    func createDemoBook() async throws -> DemoBookResponse {
        try await post(DemoBookResponse.self, path: "/api/books/demo", body: EmptyBody())
    }

    func file(bookID: String, path: String) async throws -> BookFile {
        var components = URLComponents()
        components.queryItems = [URLQueryItem(name: "path", value: path)]
        let query = components.percentEncodedQuery ?? ""
        return try await get(BookFile.self, path: "/api/books/\(bookID.apiPathComponent)/file?\(query)")
    }

    func saveFile(bookID: String, path: String, content: String, baseRevision: String) async throws {
        struct Body: Encodable { let path: String; let content: String; let baseRevision: String }
        _ = try await put(OKResponse.self, path: "/api/books/\(bookID.apiPathComponent)/file",
                          body: Body(path: path, content: content, baseRevision: baseRevision))
    }

    func saveOutlineField(bookID: String, path: String, kind: String, entityID: String, field: String, value: String, baseRevision: String) async throws -> BookFile {
        struct Body: Encodable {
            let path: String; let kind: String; let entityId: String; let field: String; let value: String; let baseRevision: String
        }
        struct Response: Decodable { let ok: Bool; let content: String }
        let response = try await patch(Response.self, path: "/api/books/\(bookID.apiPathComponent)/outline-field",
                                       body: Body(path: path, kind: kind, entityId: entityID, field: field, value: value, baseRevision: baseRevision))
        return BookFile(path: path, content: response.content)
    }

    func enableWrite(bookID: String) async throws {
        _ = try await post(BookSummary.self, path: "/api/books/\(bookID.apiPathComponent)/enable-write", body: EmptyBody())
    }

    func scaffoldMissing(bookID: String) async throws {
        _ = try await post(OKResponse.self, path: "/api/books/\(bookID.apiPathComponent)/scaffold-missing", body: EmptyBody())
    }

    func identitySegments(bookID: String, action: String, index: Int, segment: IdentitySegment, baseRevision: String) async throws {
        struct Body: Encodable { let action: String; let index: Int; let segment: IdentitySegment; let baseRevision: String }
        struct Response: Decodable { let ok: Bool; let segments: [IdentitySegment]? }
        _ = try await post(Response.self, path: "/api/books/\(bookID.apiPathComponent)/identity-segments",
                           body: Body(action: action, index: index, segment: segment, baseRevision: baseRevision))
    }

    func promoteCast(bookID: String, stem: String, initialIdentity: IdentitySegment, baseRevision: String) async throws {
        struct Body: Encodable { let initialIdentity: IdentitySegment; let baseRevision: String }
        _ = try await post(OKResponse.self, path: "/api/books/\(bookID.apiPathComponent)/cast/\(stem.apiPathComponent)/promote",
                           body: Body(initialIdentity: initialIdentity, baseRevision: baseRevision))
    }
}

// MARK: - Runs & candidates

extension NovelAPIClient {
    func previewRun(_ spec: TaskSpec) async throws -> ContextPacket {
        try await post(ContextPacket.self, path: "/api/runs/preview", body: spec)
    }

    func startRun(_ spec: TaskSpec) async throws -> Run {
        try await post(Run.self, path: "/api/runs", body: spec)
    }

    /// Slim run list: precomputed totals, no per-call records. Detail views
    /// fetch the full run by id.
    func runs(bookID: String? = nil, limit: Int = 50, offset: Int = 0) async throws -> [Run] {
        var query = "limit=\(limit)"
        if offset > 0 { query += "&offset=\(offset)" }
        if let bookID {
            return try await get([Run].self, path: "/api/books/\(bookID.apiPathComponent)/runs?\(query)")
        }
        return try await get([Run].self, path: "/api/runs?\(query)")
    }

    func run(id: String) async throws -> Run {
        try await get(Run.self, path: "/api/runs/\(id.apiPathComponent)")
    }

    func cancelRun(id: String) async throws {
        _ = try await post(OKResponse.self, path: "/api/runs/\(id.apiPathComponent)/cancel", body: EmptyBody())
    }

    /// Slim candidate list: metadata + findings + Has* flags, no bulk payloads.
    /// The review panel renders straight from it; cards fetch detail on expand.
    func candidates(bookID: String) async throws -> [Candidate] {
        try await get([Candidate].self, path: "/api/books/\(bookID.apiPathComponent)/candidates")
    }

    func candidateHistory(bookID: String, limit: Int = 50, kind: String? = nil) async throws -> [Candidate] {
        var query = "limit=\(limit)"
        if let kind { query += "&kind=\(kind.apiPathComponent)" }
        return try await get([Candidate].self, path: "/api/books/\(bookID.apiPathComponent)/candidates/history?\(query)")
    }

    func candidate(id: String) async throws -> Candidate {
        try await get(Candidate.self, path: "/api/candidates/\(id.apiPathComponent)")
    }

    func acceptCandidate(id: String, baseRevision: String, forceRiskAccept: Bool = false) async throws -> Candidate {
        try await post(Candidate.self, path: "/api/candidates/\(id.apiPathComponent)/accept",
                       body: AcceptCommand(baseRevision: baseRevision, acceptFindingIds: nil, forceRiskAccept: forceRiskAccept ? true : nil))
    }

    func rejectCandidate(id: String, reason: String) async throws {
        struct Body: Encodable { let reason: String }
        _ = try await post(OKResponse.self, path: "/api/candidates/\(id.apiPathComponent)/reject", body: Body(reason: reason))
    }
}

// MARK: - Knowledge

extension NovelAPIClient {
    func knowledgeDocs() async throws -> [KnowledgeDoc] {
        try await get([KnowledgeDoc].self, path: "/api/knowledge")
    }

    func knowledge(id: String) async throws -> KnowledgeContent {
        try await get(KnowledgeContent.self, path: "/api/knowledge/\(id.apiPathComponent)")
    }

    func saveKnowledge(id: String, update: KnowledgeUpdate) async throws {
        _ = try await put(OKResponse.self, path: "/api/knowledge/\(id.apiPathComponent)", body: update)
    }

    func restoreKnowledge(id: String) async throws -> KnowledgeContent {
        try await post(KnowledgeContent.self, path: "/api/knowledge/\(id.apiPathComponent)/restore", body: EmptyBody())
    }
}

// MARK: - Providers / settings

extension NovelAPIClient {
    func providers() async throws -> [Provider] {
        try await get([Provider].self, path: "/api/llm/configs")
    }

    func presets() async throws -> [ProviderPreset] {
        try await get([ProviderPreset].self, path: "/api/llm/presets")
    }

    func saveProvider(_ request: ProviderRequest, id: String? = nil) async throws -> Provider {
        if let id {
            return try await put(Provider.self, path: "/api/llm/configs/\(id.apiPathComponent)", body: request)
        }
        return try await post(Provider.self, path: "/api/llm/configs", body: request)
    }

    func deleteProvider(id: String) async throws {
        _ = try await delete(OKResponse.self, path: "/api/llm/configs/\(id.apiPathComponent)")
    }

    func llmModels() async throws -> [LLMModel] {
        try await get([LLMModel].self, path: "/api/llm/models")
    }

    /// Fetches the provider's model list using an unsaved form (name/key/url).
    func fetchModels(_ request: ProviderRequest) async throws -> [ProviderModel] {
        struct Response: Decodable { let models: [ProviderModel]? }
        let response = try await post(Response.self, path: "/api/llm/configs/fetch-models", body: request)
        return response.models ?? []
    }

    func refreshProviderModels(id: String) async throws -> [ProviderModel] {
        struct Response: Decodable { let models: [ProviderModel]? }
        let response = try await post(Response.self, path: "/api/llm/configs/\(id.apiPathComponent)/refresh-models", body: EmptyBody())
        return response.models ?? []
    }

    func testModel(id: String) async throws -> LLMTestResult {
        struct Body: Encodable { let modelId: String }
        return try await post(LLMTestResult.self, path: "/api/llm/test", body: Body(modelId: id))
    }

    func settings() async throws -> AppSettings {
        try await get(AppSettings.self, path: "/api/settings")
    }

    func saveAssignments(_ assignments: [ModelAssignment]) async throws -> AppSettings {
        struct Body: Encodable { let assignments: [ModelAssignment] }
        return try await put(AppSettings.self, path: "/api/settings/model-assignments", body: Body(assignments: assignments))
    }

    func saveReviewSettings(_ review: ReviewSettings) async throws -> AppSettings {
        try await put(AppSettings.self, path: "/api/settings/review", body: review)
    }

    func modelConfigs() async throws -> [ModelConfig] {
        struct Response: Decodable { let models: [ModelConfig]? }
        return try await get(Response.self, path: "/api/model-configs").models ?? []
    }

    func saveModelConfigs(_ models: [ModelConfig]) async throws -> [ModelConfig] {
        struct Response: Decodable { let models: [ModelConfig]? }
        return try await put(Response.self, path: "/api/model-configs", body: models).models ?? models
    }

    func refreshModelConfigs() async throws -> [ModelConfig] {
        struct Response: Decodable { let models: [ModelConfig]? }
        return try await post(Response.self, path: "/api/model-configs/refresh", body: EmptyBody()).models ?? []
    }

    func setLibraryRoot(_ path: String) async throws -> LibraryRootResponse {
        struct Body: Encodable { let path: String }
        return try await put(LibraryRootResponse.self, path: "/api/settings/library-root", body: Body(path: path))
    }

    func syncStatus() async throws -> SyncStatus {
        try await get(SyncStatus.self, path: "/api/sync/status")
    }

    func syncPull() async throws -> SyncStatus {
        try await post(SyncStatus.self, path: "/api/sync/pull", body: EmptyBody())
    }

    func syncPush() async throws -> SyncStatus {
        try await post(SyncStatus.self, path: "/api/sync/push", body: EmptyBody())
    }
}

struct EmptyBody: Encodable {}

struct LibraryRootResponse: Decodable {
    let ok: Bool?
    let libraryRoot: String?
    let moved: Int?
    let knowledge: Int?
}

/// /api/sync/status and the manual pull/push responses. Absent fields are
/// omitted by the Go handlers, so everything optional.
struct SyncStatus: Decodable {
    let enabled: Bool
    let syncDir: String?
    let libraryRoot: String?
    let defaultLibraryRoot: String?
    let customLibraryRoot: String?
    let lastPulledAt: String?
    let lastPushedAt: String?
    let ok: Bool?
    let changed: Bool?
}

extension String {
    var apiPathComponent: String {
        addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/?#"))) ?? self
    }
}