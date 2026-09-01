# Cymonkey macOS Extension

`pkg/macos-ext` is the user-facing Cymonkey host for macOS. It is a menu-bar
application with an embedded Safari WebExtension. It imports the reusable
`CymonkeyMacOSRuntime` library from `pkg/macos-cymonkey-helper`; the distinct
headless helper executable remains available for remote, server, and
provider-owned attachment.

## Product surface

The menu-bar application presents:

- Cymonkey and Jangolova connection state;
- start/stop controls for local managed Cymonkey attachment;
- Automation and Accessibility permission state;
- installed userscript status, with enable/disable controls only when the
  embedded browser runtime advertises execution support;
- Safari WebExtension state and a shortcut to Safari extension preferences;
- access to logs containing semantic status only, never script source,
  credentials, Apple Event payloads, or Accessibility values;
- Quit, which stops Cymonkey-owned attachment work but never quits augmented
  applications.

## Helper modes

The macOS Cymonkey configuration supports:

| Mode | Ownership |
| --- | --- |
| `external` | Cymonkey returns one-time launch material; another owner launches the distinct helper executable. |
| `managed` | The menu-bar app hosts `CymonkeyMacOSRuntime` in-process and supervises its control connection. |
| `auto` | Use the managed runtime when the local signed app is available; otherwise use external attachment. |

`auto` is the menu-bar product default. Headless Cymonkey keeps `external` as
its conservative default. Managed mode owns only the Cymonkey helper/runtime,
not Finder, Safari, Music, or another augmented application.

The app never loads a helper path from an augmentation or userscript manifest.
Production builds use the runtime linked into the signed app. A separately
launched helper must pass the existing code-signature and configuration policy.

## Package shape

```text
pkg/macos-ext/
  Package.swift                  testable menu/runtime libraries
  Sources/JangolovaMacApp/       AppKit menu-bar application
  Sources/JangolovaMacCore/      state, helper mode, userscript metadata
  Safari/                        generated Xcode containing-app project
  scripts/build-safari.sh        WXT Safari build + Apple converter/rebuild
```

The Swift package provides independently testable state and menu logic. The
shipping `.app` and `.appex` are built by Xcode because Safari WebExtensions
must be embedded extension targets with bundle identifiers, entitlements,
signing, and App Store distribution metadata.

## Safari WebExtension

WXT emits the Safari WebExtension resources from `pkg/browser-ext`. Apple's
`safari-web-extension-converter` creates the containing Xcode project once;
later builds synchronize WXT output into its extension resource directory so
the converter cannot overwrite the app's Swift package and menu customizations.
The app and extension are signed together by
the target owner. Repository CI may build unsigned development products, but
does not select a production signing identity.

The Safari extension consumes the same `@jangolova/userscript-runtime` package
as Chrome, Edge, and Firefox. Runtime execution remains capability-probed. The
Safari target does not claim userscript execution merely because it can inject
packaged content scripts.

The containing app and Safari extension communicate through the generated
native message handler and a narrowly scoped App Group record. The native side
may expose userscript metadata and state to the menu bar; source remains in
extension-owned storage unless the user performs an explicit export.

## Trust and lifecycle

- The user installs and signs/receives the containing app and Safari extension.
- macOS owns Automation, Accessibility, App Sandbox, and website-access consent.
- Safari owns per-site extension access.
- Cymonkey owns userscript lifecycle semantics. The Cymonkey extension owns
  approval UI, persistence, runtime probing, native registration, and its
  control connection.
- Target applications and Safari tabs remain caller/user-owned.
- App termination unregisters in-process control work but does not remove the
  Safari extension or userscript records.
