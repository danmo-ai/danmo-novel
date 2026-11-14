import Foundation
import DanmoNovel

/// Paths the app reads and writes through iCloud Documents.
///
/// The Go core receives these through `DanmonovelSetPaths`; SQLite and logs
/// deliberately stay in the app-private home.
struct SyncLocation: Equatable {
    let syncDir: URL
    let library: URL
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
    /// The container's Documents folder is the shared root; macOS resolves the
    /// same directory as `iCloud~com~danmo~novel/Documents`.
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
            // Handed over through the Go binding, not setenv: Go snapshots the
            // environment when its runtime starts, so a libc setenv here would
            // be invisible to the core and it would silently read its sandbox.
            _ = DanmonovelSetPaths(location.library.path, location.syncDir.path)
            await materializeSharedRoot(at: location.syncDir)
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

    /// iCloud keeps files as placeholders until something asks for them, and a
    /// placeholder is a hidden `<name>.icloud` entry: the real path does not
    /// exist, so `FileManager.enumerator` (which skips hidden files) never sees
    /// it and `os.ReadFile` reports "no such file".
    ///
    /// That is not merely a slow first launch. A settings merge that cannot see
    /// `model-settings.json` treats this device as the only writer, and a book
    /// scan sees an empty shelf. So every file under the shared root is resolved
    /// to its real URL and requested before the core starts.
    ///
    /// Timing out is not an error: startup continues with whatever is local and
    /// the next launch retries.
    private func materializeSharedRoot(at root: URL) async {
        let fileManager = FileManager.default
        let deadline = Date().addingTimeInterval(20)
        var requested = Set<URL>()
        while true {
            let pending = await pendingDownloads(under: root, requested: &requested)
            if pending == 0 || Date() >= deadline { return }
            try? await Task.sleep(nanoseconds: 500_000_000)
        }
    }

    /// Requests every undownloaded file under `root`, including placeholders,
    /// and returns how many are still pending.
    private func pendingDownloads(under root: URL, requested: inout Set<URL>) async -> Int {
        let fileManager = FileManager.default
        var pending = 0
        var stack = [root]
        while let dir = stack.popLast() {
            let entries = (try? fileManager.contentsOfDirectory(
                at: dir,
                includingPropertiesForKeys: [.isDirectoryKey, .ubiquitousItemDownloadingStatusKey],
                options: []
            )) ?? []
            for entry in entries {
                let name = entry.lastPathComponent
                // `.book.md.icloud` stands for `book.md`, which does not exist
                // until the download finishes.
                if name.hasPrefix("."), name.hasSuffix(".icloud") {
                    let real = dir.appending(path: String(name.dropFirst().dropLast(".icloud".count)))
                    pending += 1
                    if requested.insert(real).inserted {
                        try? fileManager.startDownloadingUbiquitousItem(at: real)
                    }
                    continue
                }
                let values = try? entry.resourceValues(forKeys: [.isDirectoryKey, .ubiquitousItemDownloadingStatusKey])
                if values?.isDirectory == true {
                    stack.append(entry)
                    continue
                }
                if values?.ubiquitousItemDownloadingStatus != .current {
                    pending += 1
                    if requested.insert(entry).inserted {
                        try? fileManager.startDownloadingUbiquitousItem(at: entry)
                    }
                }
            }
        }
        return pending
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
