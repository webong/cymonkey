// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "CymonkeyMacBrowserAdapter",
    platforms: [.macOS(.v13)],
    products: [
        .library(name: "JangolovaMacCore", targets: ["JangolovaMacCore"]),
    ],
    dependencies: [
        .package(path: "../macos-cymonkey-helper"),
    ],
    targets: [
        .target(
            name: "JangolovaMacCore",
            dependencies: [
                .product(name: "CymonkeyMacOSRuntime", package: "macos-cymonkey-helper"),
            ]
        ),
        .testTarget(name: "JangolovaMacCoreTests", dependencies: ["JangolovaMacCore"]),
    ]
)
