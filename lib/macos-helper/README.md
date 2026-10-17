# Cymonkey macOS Helper

This package provides the caller-owned native binding for Cymonkey's `viewer`
domain with runtime `macos-app`, plus Swift APIs for a macOS browser host. A
native host builds, signs, configures, and launches the executable. Jangolova
never launches or terminates it.

The helper connects outward to an authenticated WebSocket control endpoint and
implements the same five `cymonkey/v1alpha1` operations used by other
Cymonkey domains: `hello`, `capabilities`, `describe`, `act`, and `events`.

It exposes only:

- application commands explicitly mapped to four-character Apple Event class
  and event identifiers in the owner configuration;
- bounded Accessibility queries for configured application bundle IDs;
- actions reported by the selected Accessibility element;
- attributes that Accessibility reports as settable and that policy allows.
- optional, allowlisted window viewer surfaces with Screen Recording-gated PNG
  capture and Accessibility-gated, policy-bounded pointer or keyboard input.

It does not accept AppleScript source, raw Apple Event descriptors, arbitrary
bundle identifiers, process launch requests, raw display-server handles, or
system-wide Accessibility tree dumps.

## Swift library APIs

Import `CymonkeyMacOSRuntime` to use the native runtime, the managed control
connection, and the userscript catalog from a caller-owned macOS app.
`ManagedRuntimeController` starts and stops the authenticated control
connection from a `ManagedRuntimeConfiguration`. `HelperMode` selects an
external or managed connection based on the host's available runtime.
`UserscriptCatalog` stores source-free `UserscriptSummary` records in standard
or app-group `UserDefaults`; the host supplies its own UI and lifecycle.

## App-owned WKWebView scripts

Import the separate `MacOSWebViewScripts` product in an app that owns a
`WKWebView`. Register approved source on the web view's content controller
before creating the view or starting navigation:

```swift
import MacOSWebViewScripts
import WebKit

let configuration = WKWebViewConfiguration()
let installer = MacOSWebViewScriptInstaller(
    contentController: configuration.userContentController)
try installer.installApprovedScript(
    id: "reading-tools", source: reviewedSource,
    allowedOrigins: [URL(string: "https://example.com")!],
    timing: .documentStart)
let webView = WKWebView(frame: .zero, configuration: configuration)
```

The installer uses `WKUserScript` at `.documentStart` or `.documentEnd` and
checks the page's exact HTTPS origin before executing. It defaults to the main
frame. The host reviews source and chooses which content controller receives
the script. A controller shared by multiple web views shares its scripts.
WebKit has no per-script removal API for `WKUserContentController`, so an
installed script stays on that controller for future navigations; use a fresh
controller and web view to retire it. The installer never removes the host's
other scripts. This is a library for an app-owned web view; the macOS viewer
executable does not inject into arbitrary apps or Safari.

## Native browser cookies

Reading Safari, Chromium and Firefox cookie stores is done by ext's browser
adapters through `cymy cookies`, not by this package. The earlier
`MacOSBrowserCookies` product and its SweetCookieKit snapshot are removed.

## Build and test

```sh
swift build --package-path pkg/macos-helper
swift test --package-path pkg/macos-helper
```

The resulting command-line executable is intentionally unsigned in repository
builds. The target owner signs or embeds it using its own Apple Developer
identity and provisioning profile. The example entitlements file shows the
Automation shape for an intentionally supported scripting target. An
Accessibility-enabled helper normally runs outside App Sandbox and relies on
the normal user-granted Accessibility TCC permission; a sandboxed,
Apple-Events-only distribution needs its own network-client and scripting
entitlements. The owner must choose and test the distribution model rather than
copying a universal entitlement set that macOS does not provide.

An owner with an installed signing identity can produce a hardened-runtime
release binary with:

```sh
CODESIGN_IDENTITY="Developer ID Application: Example Corp (TEAMID)" \
  pkg/macos-helper/scripts/build-and-sign.sh /owner/output/cymonkey-macos-helper
```

Set `CYMONKEY_ENTITLEMENTS` only when the chosen distribution model requires a
reviewed entitlement file. The script rejects ad-hoc signing and verifies the
result. Jangolova never selects the identity or performs this step at runtime.

## Configuration

Start from `helper-config.example.json`. Configuration contains allowlists and
semantic-to-native mappings, never credentials. The launcher provides:

- `CYMONKEY_CONTROL_URL`: an absolute `ws://` loopback or `wss://`
  control endpoint;
- `CYMONKEY_CONTROL_TOKEN`: a short-lived bearer token;
- `CYMONKEY_PROTOCOL`: exactly `cymonkey/v1alpha1`;
- `CYMONKEY_CONFIG`: an absolute path to the helper configuration.

Non-loopback plaintext WebSocket endpoints and credentials embedded in URLs
are rejected. Disconnecting the socket ends the helper without quitting any
target application.

Accessibility consent is checked with the macOS Accessibility API. The helper
can request the normal system prompt only when `promptForAccessibilityConsent`
is enabled. Apple Events are sent only for configured commands; macOS remains
responsible for Automation consent.

When the optional `viewer` configuration is enabled, Screen Recording consent
is negotiated separately for `display.capture`; Accessibility is negotiated
separately for pointer and keyboard input. The helper advertises neither
viewer capability family until the corresponding consent is granted. Viewer
surfaces are restricted to windows owned by the configured bundle IDs and are
attachment-scoped.
