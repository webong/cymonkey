// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "CymonkeyMacOSHelper",
    platforms: [.macOS(.v13)],
    products: [
        .library(name: "CymonkeyMacOSRuntime", targets: ["CymonkeyMacOSRuntime"]),
        .executable(name: "cymonkey-macos-helper", targets: ["CymonkeyMacOSHelper"]),
    ],
    targets: [
        .target(name: "CymonkeyMacOSRuntime"),
        .executableTarget(
            name: "CymonkeyMacOSHelper",
            dependencies: ["CymonkeyMacOSRuntime"]
        ),
        .testTarget(
            name: "CymonkeyMacOSRuntimeTests",
            dependencies: ["CymonkeyMacOSRuntime"]
        ),
    ]
)
