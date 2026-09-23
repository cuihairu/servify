// swift-tools-version:5.10
// Servify iOS SDK（D4：Swift + SwiftUI；D9：零三方依赖由"无 dependencies 声明"结构性保证）。
import PackageDescription

let package = Package(
    name: "ServifyKit",
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
