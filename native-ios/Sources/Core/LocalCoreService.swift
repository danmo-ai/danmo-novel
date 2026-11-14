import Foundation
import DanmoNovel

/// Owns one app-private Go core. REST requests await startup and then execute
/// on the same serial queue as lifecycle operations.
@Observable
final class LocalCoreService: @unchecked Sendable {
    static let shared = LocalCoreService()
    private let home: URL?
    private let lock = NSLock()
    private let queue = DispatchQueue(label: "com.danmo.novel.core", qos: .userInitiated)
    private var startup: Task<Void, Error>?
    private(set) var isReady = false
    private(set) var error: String?

    init(home: URL? = nil) { self.home = home }

    func start() {
        lock.lock()
        if startup != nil {
            lock.unlock()
            return
        }
        startup = Task { try await self.initialize() }
        lock.unlock()
    }

    private func initialize() async throws {
        let home = try self.home ?? FileManager.default.url(
            for: .applicationSupportDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        ).appending(path: "DanmoNovel", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        let result = try await withCheckedThrowingContinuation { continuation in
            queue.async {
                continuation.resume(with: Result { try CoreResult(DanmonovelStart(home.path)) })
            }
        }
        guard result.ok else { throw APIError.message("本地内核启动失败") }
        Task { @MainActor in self.isReady = true }
    }

    private func waitUntilReady() async throws {
        start()
        lock.lock()
        let startup = self.startup
        lock.unlock()
        guard let startup else { throw APIError.coreNotReady }
        do { try await startup.value }
        catch {
            Task { @MainActor in self.error = error.localizedDescription }
            throw error
        }
    }

    func request(method: String, path: String, body: String = "") async throws -> CoreResponse {
        try await waitUntilReady()
        return try await withCheckedThrowingContinuation { continuation in
            queue.async {
                let raw = DanmonovelRequest(method, path, body, "")
                do { continuation.resume(returning: try JSONDecoder().decode(CoreResponse.self, from: Data(raw.utf8))) }
                catch { continuation.resume(throwing: error) }
            }
        }
    }

    func stop() {
        lock.lock()
        startup = Task { }
        lock.unlock()
        Task { @MainActor in self.isReady = false }
        queue.async { _ = DanmonovelStop() }
    }
}

struct CoreResult: Decodable {
    let ok: Bool
    let error: String?

    init(_ raw: String) throws {
        self = try JSONDecoder().decode(Self.self, from: Data(raw.utf8))
        if let error, !error.isEmpty { throw APIError.message(error) }
    }
}

struct CoreResponse: Decodable {
    let status: Int
    let body: String
}
