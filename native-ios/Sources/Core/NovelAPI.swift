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
            client?.urlProtocol(self, didFailWithError: APIError.message("无效的本地 API 地址"))
            return
        }
        var path = url.path
        if let query = url.query { path += "?" + query }
        let body = request.httpBody.flatMap { String(data: $0, encoding: .utf8) } ?? ""
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
    }

    func get<T: Decodable>(_ type: T.Type, path: String) async throws -> T {
        try await perform(type, method: "GET", path: path)
    }

    func post<T: Decodable, Body: Encodable>(_ type: T.Type, path: String, body: Body) async throws -> T {
        let data = try encoder.encode(body)
        return try await perform(type, method: "POST", path: path, body: String(decoding: data, as: UTF8.self))
    }

    private func perform<T: Decodable>(_ type: T.Type, method: String, path: String, body: String = "") async throws -> T {
        guard let url = URL(string: "danmo://local\(path)") else { throw APIError.message("无效的 API 路径") }
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
        case .coreNotReady: "本地工作区尚未就绪，请稍后重试。"
        case .http(let status): "本地服务返回错误（\(status)）。"
        case .message(let message), .decoding(let message): message
        }
    }
}

struct APIErrorBody: Decodable { let error: String }

extension NovelAPIClient {
    func books() async throws -> [BookSummary] {
        try await get([BookSummary].self, path: "/api/books")
    }

    func book(id: String) async throws -> BookDetail {
        // view=list is the navigation projection: summary + unit metadata only.
        // The full snapshot is 3–4 MB for a large book and made every tap stall.
        try await get(BookResponse.self, path: "/api/books/\(id.apiPathComponent)?view=list").book
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

    func providers() async throws -> [Provider] {
        try await get([Provider].self, path: "/api/llm/configs")
    }

    func saveProvider(_ request: ProviderRequest, id: String? = nil) async throws -> Provider {
        if let id {
            return try await perform(Provider.self, method: "PUT", path: "/api/llm/configs/\(id.apiPathComponent)", body: String(decoding: try encoder.encode(request), as: UTF8.self))
        }
        return try await post(Provider.self, path: "/api/llm/configs", body: request)
    }

    func runs() async throws -> [RunRecord] {
        try await get([RunRecord].self, path: "/api/runs")
    }
}

struct EmptyBody: Encodable {}

extension String {
    var apiPathComponent: String {
        addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/?#"))) ?? self
    }
}
