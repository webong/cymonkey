// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "CymonkeyMacOSHelper",
    platforms: [.macOS(.v13)],
    products: [
        .executable(name: "cymonkey-device-audio", targets: ["CymonkeyDeviceAudio"]),
        .library(name: "CymonkeyMacOSRuntime", targets: ["CymonkeyMacOSRuntime"]),
        .library(name: "MacOSWebViewScripts", targets: ["MacOSWebViewScripts"]),
        .executable(name: "cymonkey-macos-helper", targets: ["CymonkeyMacOSHelper"]),
    ],
    dependencies: [
        .package(url: "https://github.com/webong/ext.git", revision: "a1e69d860f610a5a0c1567a18c61fbc03780c9d8"),
    ],
    targets: [
        .executableTarget(name: "CymonkeyDeviceAudio", dependencies: [
            "CymonkeyMacOSRuntime",
            .product(name: "ExtAudio", package: "ext"),
            .product(name: "ExtSystem", package: "ext"),
        ]),
        .target(name: "CymonkeyMacOSRuntime", dependencies: [
            .product(name: "ExtClipboard", package: "ext"),
        ]),
        .target(name: "MacOSWebViewScripts"),
        .executableTarget(
            name: "CymonkeyMacOSHelper",
            dependencies: ["CymonkeyMacOSRuntime"]
        ),
        .testTarget(
            name: "CymonkeyMacOSRuntimeTests",
            dependencies: ["CymonkeyMacOSRuntime"]
        ),
        .testTarget(
            name: "MacOSWebViewScriptsTests",
            dependencies: ["MacOSWebViewScripts"]
        ),
    ]
)
