// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "tauri-plugin-localcore",
    platforms: [.iOS(.v15)],
    products: [.library(name: "tauri-plugin-localcore", type: .static, targets: ["tauri-plugin-localcore"])],
    dependencies: [.package(name: "Tauri", path: "../.tauri/tauri-api")],
    targets: [
        .target(name: "tauri-plugin-localcore", dependencies: [.byName(name: "Tauri"), .byName(name: "DanmoNovel")], path: "Sources"),
        .binaryTarget(name: "DanmoNovel", path: "Frameworks/DanmoNovel.xcframework")
    ]
)
