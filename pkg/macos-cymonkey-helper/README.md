# Cymonkey macOS Helper

This package is the caller-owned native binding for Cymonkey's `viewer`
domain with runtime `macos-app`. It is a Swift executable that a native host builds, signs, configures,
and launches. Jangolova never launches or terminates it.

The helper connects outward to an authenticated WebSocket control endpoint and
implements the same five `jangolova.cymonkey/v1alpha2` operations used by other
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

## Build and test

```sh
swift build --package-path pkg/macos-cymonkey-helper
swift test --package-path pkg/macos-cymonkey-helper
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
  pkg/macos-cymonkey-helper/scripts/build-and-sign.sh /owner/output/cymonkey-macos-helper
```

Set `CYMONKEY_ENTITLEMENTS` only when the chosen distribution model requires a
reviewed entitlement file. The script rejects ad-hoc signing and verifies the
result. Jangolova never selects the identity or performs this step at runtime.

## Configuration

Start from `helper-config.example.json`. Configuration contains allowlists and
semantic-to-native mappings, never credentials. The launcher provides:

- `JANGOLOVA_CYMONKEY_CONTROL_URL`: an absolute `ws://` loopback or `wss://`
  control endpoint;
- `JANGOLOVA_CYMONKEY_CONTROL_TOKEN`: a short-lived bearer token;
- `JANGOLOVA_CYMONKEY_PROTOCOL`: exactly `jangolova.cymonkey/v1alpha2`;
- `JANGOLOVA_CYMONKEY_CONFIG`: an absolute path to the helper configuration.

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
