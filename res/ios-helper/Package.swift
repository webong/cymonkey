// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "CymonkeyIOSHelper",
    platforms: [.iOS(.v15), .macOS(.v12)],
    products: [.library(name: "CymonkeyIOSHelper", targets: ["CymonkeyIOSHelper"])],
    dependencies: [
        .package(url: "https://github.com/webong/ext.git", revision: "e2a1e6979a809e319cbe32affc15c4cdf434b468"),
    ],
    targets: [
        .target(name: "CymonkeyIOSHelper", dependencies: [
            .product(name: "ExtAudio", package: "ext"),
            .product(name: "ExtSystem", package: "ext"),
        ]),
        .testTarget(name: "CymonkeyIOSHelperTests", dependencies: ["CymonkeyIOSHelper"]),
    ]
)
