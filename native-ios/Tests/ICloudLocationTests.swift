import XCTest
@testable import DanmoNovelApp

final class ICloudLocationTests: XCTestCase {
    func testMapsContainerIntoSharedLayout() throws {
        let container = URL(fileURLWithPath: "/tmp/cloud-container", isDirectory: true)
        let location = try XCTUnwrap(ICloudLocation.syncLocation(containerURL: container))
        // The container's Documents folder is the folder iCloud Drive shows
        // under the container name, so no extra app folder is nested inside it.
        XCTAssertEqual(location.syncDir.path, "/tmp/cloud-container/Documents")
        XCTAssertEqual(location.library.path, "/tmp/cloud-container/Documents/library")
        XCTAssertEqual(location.environment["NOVEL_LIBRARY"], location.library.path)
        XCTAssertEqual(location.environment["NOVEL_SYNC_DIR"], location.syncDir.path)
        XCTAssertEqual(location.environment.count, 2, "only the library and sync dir are shared; the database stays local")
    }

    func testFallsBackWhenNoContainer() {
        XCTAssertNil(ICloudLocation.syncLocation(containerURL: nil))
    }

    func testDecodesSyncStatusWithoutProviderData() throws {
        let data = Data(#"{"enabled":true,"libraryRoot":"/x/library","syncDir":"/x","lastPulledAt":"2026-11-14T07:00:00Z"}"#.utf8)
        let status = try JSONDecoder().decode(SyncStatus.self, from: data)
        XCTAssertTrue(status.enabled)
        XCTAssertEqual(status.libraryRoot, "/x/library")
        XCTAssertEqual(status.lastPulledAt, "2026-11-14T07:00:00Z")
        XCTAssertNil(status.lastPushedAt)
        XCTAssertNil(status.changed)
    }

    // A host that passes its own home is an isolated workspace. It must not
    // connect to iCloud even on an entitled, signed-in build, or a test run
    // would write into the author's real book library.
    func testInjectedHomeNeverResolvesICloud() {
        XCTAssertFalse(ICloudLocation.shouldResolve(injectedHome: URL(fileURLWithPath: "/tmp/scratch")))
        XCTAssertTrue(ICloudLocation.shouldResolve(injectedHome: nil))
    }

    func testIsolatedCoreStaysOffICloud() async throws {
        let home = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }
        let core = LocalCoreService(home: home)
        core.start()
        for _ in 0..<100 where !core.isReady {
            try await Task.sleep(for: .milliseconds(50))
        }
        XCTAssertTrue(core.isReady, core.error ?? "core did not start")
        XCTAssertFalse(core.syncEnabled, "an injected home must not use iCloud")
        XCTAssertNil(core.syncPath)
        core.stop()
    }
}
