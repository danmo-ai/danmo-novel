import Foundation
import DanmoNovel

/// Paths the app reads and writes through iCloud Documents.
struct SyncLocation: Equatable {
    let syncDir: URL
    let library: URL

    /// Environment the Go core reads for the library and the shared settings
    /// folder. SQLite and logs deliberately stay in the app-private home.
    var environment: [String: String] {
        [
            "NOVEL_LIBRARY": library.path,
            "NOVEL_SYNC_DIR": syncDir.path,
        ]
    }
}

enum ICloudLocation {
    static let containerIdentifier = "iCloud.com.danmo.novel"

    /// Whether this process should connect to the account's iCloud container.
    ///
    /// A host that supplies its own home is an isolated workspace — unit tests,
    /// the embedded core, a scratch run. It must never reach into the real
    /// iCloud library even when the build is entitled and someone is signed in;
    /// otherwise `make test` on a device would write into the author's books.
    /// This mirrors the Go side, where only the default home owns iCloud.
    static func shouldResolve(injectedHome: URL?) -> Bool { injectedHome == nil }

    /// Pure mapping from a container URL. Kept separate from `resolve()` so the
    /// layout is testable without an entitled device.
    ///
    /// The container's Documents folder is what iCloud Drive shows under
    /// NSUbiquitousContainerName ("DanmoNovel"), so it already is the shared
    /// root: `iCloud Drive/DanmoNovel`. macOS reaches the same directory as
    /// `CloudDocs/DanmoNovel` and derives its paths from there.
    static func syncLocation(containerURL: URL?) -> SyncLocation? {
        guard let containerURL else { return nil }
        let syncDir = containerURL.appending(path: "Documents", directoryHint: .isDirectory)
        return SyncLocation(
            syncDir: syncDir,
            library: syncDir.appending(path: "library", directoryHint: .isDirectory)
        )
    }

    /// `url(forUbiquityContainerIdentifier:)` blocks while it talks to the
    /// iCloud daemon, so it is kept off the cooperative pool. Returns nil when
    /// iCloud is unavailable — not signed in, entitlement missing, or an
    /// unsigned simulator build — which is the signal to use the local sandbox.
    static func resolve() async -> SyncLocation? {
        let container: URL? = await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .utility).async {
                continuation.resume(returning: FileManager.default.url(forUbiquityContainerIdentifier: containerIdentifier))
            }
        }
        return syncLocation(containerURL: container)
    }
}

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

    /// True when the book library and model settings live in iCloud.
    private(set) var syncEnabled = false
    private(set) var syncPath: String?
    /// Why iCloud was not used. Shown in Settings; nil when sync is enabled.
    private(set) var syncUnavailableReason: String?

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

        // The library is shared; SQLite is not. `home` above stays the process
        // home for the database and logs, while the Go core receives the iCloud
        // library through NOVEL_LIBRARY.
        let isolated = !ICloudLocation.shouldResolve(injectedHome: self.home)
        let location = isolated ? nil : await ICloudLocation.resolve()
        if let location {
            try? FileManager.default.createDirectory(at: location.library, withIntermediateDirectories: true)
            try? FileManager.default.createDirectory(at: location.syncDir, withIntermediateDirectories: true)
            for (key, value) in location.environment { setenv(key, value, 1) }
            await materializeLibrary(at: location.library)
            await MainActor.run {
                self.syncEnabled = true
                self.syncPath = location.syncDir.path
                self.syncUnavailableReason = nil
            }
        } else {
            await MainActor.run {
                self.syncEnabled = false
                self.syncPath = nil
                self.syncUnavailableReason = isolated
                    ? "独立工作区：不连接 iCloud"
                    : "iCloud 不可用（未登录或该构建未带 iCloud 能力）"
            }
        }

        let result = try await withCheckedThrowingContinuation { continuation in
            queue.async {
                continuation.resume(with: Result { try CoreResult(DanmonovelStart(home.path)) })
            }
        }
        guard result.ok else { throw APIError.message("本地内核启动失败") }
        Task { @MainActor in self.isReady = true }
    }

    /// iCloud keeps files as placeholders until something asks for them. The Go
    /// core reads book files directly and has no NSFileCoordinator bridge, so
    /// request every book file before starting and give the daemon a bounded
    /// window to deliver them. Timing out is not an error: startup continues and
    /// the next launch retries.
    private func materializeLibrary(at library: URL) async {
        let fileManager = FileManager.default
        let keys: Set<URLResourceKey> = [.isDirectoryKey, .ubiquitousItemDownloadingStatusKey]
        let deadline = Date().addingTimeInterval(15)
        var requested = Set<URL>()
        while true {
            var pending = 0
            if let walker = fileManager.enumerator(at: library, includingPropertiesForKeys: Array(keys)) {
                for case let url as URL in walker {
                    guard let values = try? url.resourceValues(forKeys: keys), values.isDirectory != true else { continue }
                    guard values.ubiquitousItemDownloadingStatus != .current else { continue }
                    pending += 1
                    if requested.insert(url).inserted {
                        try? fileManager.startDownloadingUbiquitousItem(at: url)
                    }
                }
            }
            if pending == 0 || Date() >= deadline { return }
            try? await Task.sleep(nanoseconds: 400_000_000)
        }
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
