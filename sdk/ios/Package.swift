// swift-tools-version:5.10
// Servify iOS SDK（D4：Swift + SwiftUI；D9：零三方依赖由"无 dependencies 声明"结构性保证）。
import PackageDescription

let package = Package(
    name: "ServifyKit",
    // D4：最低 iOS 15（覆盖率论证见设计文档）；macOS 12 仅 CI 测试面（async/await 与
    // URLSessionWebSocketTask 的 availability 需要 10.15+，12 与 iOS 15 API 面对齐）。
    // 不声明 platforms 时 SwiftPM 默认 macOS 10.13，availability 检查在 macos job 全炸。
    platforms: [
        .iOS(.v15),
        .macOS(.v12),
    ],
    products: [
        .library(name: "ServifyKit", targets: ["ServifyKit"]),
    ],
    targets: [
        .target(name: "ServifyKit", path: "Sources/ServifyKit"),
        .testTarget(
            name: "ServifyKitTests",
            dependencies: ["ServifyKit"],
            path: "Tests/ServifyKitTests"
        ),
    ]
)
