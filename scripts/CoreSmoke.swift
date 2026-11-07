import Foundation
import DanmoNovel

let home = FileManager.default.temporaryDirectory.appendingPathComponent("danmo-core-smoke-\(UUID().uuidString)")
try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
precondition(DanmonovelStart(home.path) == #"{"ok":true}"#)
let response = DanmonovelRequest("GET", "/api/system/health", "", "")
precondition(response.contains(#""status":200"#), response)
precondition(response.contains(#"\"ok\":true"#), response)
precondition(FileManager.default.fileExists(atPath: home.appendingPathComponent("novel.db-wal").path))
precondition(DanmonovelStop() == #"{"ok":true}"#)
print("iOS native core + SQLite WAL passed")
