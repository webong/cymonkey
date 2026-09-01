# Cymonkey macOS Extension

This package contains the macOS menu-bar host and Safari WebExtension container
for Cymonkey. It uses Jangolova's reusable macOS integration libraries. The architecture and ownership model are documented in
`docs/macos-extension.md`.

The Swift package imports the reusable `CymonkeyMacOSRuntime` library. It does
not replace the separate `cymonkey-macos-helper` executable used by headless or
externally managed environments.

```sh
swift build --package-path pkg/macos-ext
swift test --package-path pkg/macos-ext
```

The SwiftPM executable is a development/test menu-bar host. The distributable
`.app` and embedded Safari `.appex` are built from the Xcode project under
`Safari/`, generated or refreshed by `scripts/build-safari.sh`. Production
signing remains caller/target-owner supplied.
