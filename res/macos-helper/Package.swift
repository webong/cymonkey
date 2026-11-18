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
    targets: [
        .executableTarget(name: "CymonkeyDeviceAudio", dependencies: ["CymonkeyMacOSRuntime"]),
        .target(name: "CymonkeyMacOSRuntime"),
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
