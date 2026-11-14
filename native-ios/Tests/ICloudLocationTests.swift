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
}
