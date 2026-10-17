// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "CymonkeyIOSHelper",
    platforms: [.iOS(.v15), .macOS(.v12)],
    products: [.library(name: "CymonkeyIOSHelper", targets: ["CymonkeyIOSHelper"])],
    targets: [
        .target(name: "CymonkeyIOSHelper"),
        .testTarget(name: "CymonkeyIOSHelperTests", dependencies: ["CymonkeyIOSHelper"]),
    ]
)
