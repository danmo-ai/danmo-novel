// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "DanmoNovelCore",
    platforms: [.iOS(.v17)],
    products: [.library(name: "DanmoNovelCore", targets: ["DanmoNovel"])],
    targets: [.binaryTarget(name: "DanmoNovel", path: "../out/mobile/DanmoNovel.xcframework")]
)
