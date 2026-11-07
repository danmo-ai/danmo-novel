import Tauri
import UIKit
import DanmoNovel

private class LocalCorePlugin: Plugin {
  @objc public func bootstrap(_ invoke: Invoke) throws {
    let args = try invoke.parseArgs(BootstrapArgs.self)
    let home = URL(fileURLWithPath: args.home, isDirectory: true)
    try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
    let result = DanmonovelStart(home.path)
    if result.contains("\"error\"") {
      invoke.reject(result)
    } else {
      invoke.resolve(["ok": true])
    }
    // Keep the device awake only while an LLM run is active in the UI.
    DispatchQueue.main.async { UIApplication.shared.isIdleTimerDisabled = true }
  }

  @objc public func request(_ invoke: Invoke) throws {
    let args = try invoke.parseArgs(RequestArgs.self)
    let raw = DanmonovelRequest(args.method, args.path, args.body ?? "", "")
    invoke.resolve(["value": raw])
  }

  deinit {
    _ = DanmonovelStop()
    DispatchQueue.main.async { UIApplication.shared.isIdleTimerDisabled = false }
  }
}

private class BootstrapArgs: Decodable {
  let home: String
}

private class RequestArgs: Decodable {
  let method: String
  let path: String
  let body: String?
}

@_cdecl("init_plugin_localcore")
public func initPlugin() -> Plugin {
  return LocalCorePlugin()
}
