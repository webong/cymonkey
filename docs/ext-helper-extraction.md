# Ext helper extraction

This is the implementation map for removing generic platform mechanics from
Cymonkey's five `res/*-helper` packages. Cymonkey remains the operator: it
selects caller-owned targets, enforces grants and approvals, maps semantic
capabilities, and coordinates execution. Ext owns reusable web and operating-
system mechanisms. An ext primitive must not acquire Cymonkey's protocol,
policy decisions, credentials, or target lifecycle as part of extraction.

## Current overlap

The inventory below distinguishes an ext implementation from an ext-shaped
gap. Delete a duplicated native implementation only after the replacement is
linked and its contract/build checks pass. A cross-build does not count as
live platform acceptance; report that gap before claiming full support.

| Cymonkey source | Ext destination or existing equivalent | Cymonkey code to retain | State |
| --- | --- | --- | --- |
| `res/macos-helper/Sources/CymonkeyMacOSRuntime/{Accessibility,AppleEvents,Viewer,Clipboard,DeviceMedia,DevicePlayback}.swift` | `res/system/capture/{surface,clipboard,audio}/ios` | Semantic app/window capability mapping, configured allowlists, consent decisions, result receipts, and control connection | Audio playback, display/system-audio/microphone capture, and clipboard mechanics now consume pinned ext Swift products. Accessibility, Apple Events, and viewer still need adaptation. |
| `res/macos-helper/Sources/MacOSWebViewScripts` | `res/web/ios` (`ExtWebKit` and reviewed script rules) | Approval and Cymonkey surface registration | Existing ext workspace; compare registration lifetime and current-page behavior |
| `res/windows-helper/Media` and native capture/input in `src/windowshelper` | `res/system/capture/{audio,surface}/windows` | Cymonkey window policy, five-method dispatch, and target-scoped receipts | Media process now consumes `Ext.Audio.Windows`; native window capture/input still need parity and Windows execution tests. |
| `res/windows-helper/WebView2` | `res/web/windows` (`Ext.Web.Windows`) | Approved-action mapping and host-selected profile scope | Wrapper now consumes `Ext.Web.Windows` and retains origin and approval policy; Windows execution tests still required. |
| `res/ios-helper` WebView/scripts/cookies and device capture/playback | `res/web/ios` (`ExtWeb`, `ExtWebKit`) and `res/system/capture/{surface,audio}/ios` | Cymonkey `hello/capabilities/describe/act/events`, surface IDs, approval callback, and transport | Playback and in-app ReplayKit capture consume pinned ext iOS products; WebView, userscript, cookie, and broadcast-extension integration still need parity work. |
| `res/android-helper` WebView/userscripts and device capture/playback | `res/web/android` and `res/system/capture/{surface,audio}/android` | Cymonkey runtime protocol, approved-action routing, surface IDs, and transport | Projection and playback delegate to the checksum-pinned Ext Android 0.2.0 AAR; WebView and userscript mapping still need parity work. The embedding app must also supply the AAR at runtime. |
| `res/browser-helper/src/{extension-manager,userscripts,runtime,tabs}.ts` | Existing `pkg/ext-web/{native,userscripts,workspace}` APIs | Cymonkey augmentation ownership, approval, target selection, and audit | Generic extension management and userscript manager exports removed; tab messaging, scoped storage, script registration, and network-rule ownership consume ext-web. |
| `res/browser-helper/src/{page-bridge,sandbox-channel,sandbox-storage}.ts` | Existing `pkg/ext-web/bridge` plus any generic storage shim that proves reusable | Cymonkey page-global vocabulary and page-safe action filtering | Split generic channel mechanics from Cymonkey protocol and policy |
| `res/browser-helper/src/{dom-page,media-broker,cookies}.ts` | Ext web/system APIs only where a genuinely equivalent primitive exists | Cymonkey semantic DOM actions and scoped permission/approval flow | Review individually; do not move product behavior on directory-name similarity |

`res/browser-helper/src/{approvals,augmentations,augmentation-runtime-host,control-gate,engine-router,policy}.ts`
are Cymonkey execution/coordination until an individual function proves
product-neutral. `outbound-control.ts` carries Cymonkey's authenticated control
protocol; `xallet-spook.ts` is an optional product integration, not a generic
ext browser primitive.

The same test applies outside `res/*-helper`: `src/board` should use ext's
`res/system` for media, clipboard, capture, storage, and host observation where
the ext implementation meets Board's grant and evidence requirements. Board's
provider/session grants and Cymonkey domain adaptation stay here.

## Migration order

1. Replace exact native duplicates first: Windows media, Windows WebView2,
   then the macOS and mobile capture/WebView primitives. Do not introduce a
   second native backend while migrating.
2. Consume ext's existing `pkg/ext-web` JavaScript package for browser-native
   management, workspace, userscript, tab-messaging, and bridge mechanisms.
   Remove generic Cymonkey implementations only after the ext release is a
   pinned, installable dependency and parity tests pass. No fallback copy or
   compatibility layer should remain.
3. For each replacement, run the ext unit/conformance tests and the existing
   Cymonkey helper tests, followed by a real-platform smoke test. Preserve
   denial, stale-target, revision, and lifecycle behavior—not just happy paths.
4. Remove an old source file or package only when no Cymonkey or downstream
   import still depends on its implementation. Update build and package docs
   in the same change.

The owner confirmed there are no old browser registrations. The workspace
cutover therefore uses ext-web's namespaces and IDs directly: no legacy-key
reads, `cm-*` script-ID fallback, or migration layer. Cymonkey delegates batched
script registration and `excludeMatches` to ext-web v0.1.1. The network adapter now needs
`getDynamicRules` as well as `updateDynamicRules` for rollback on a failed
ownership save. Its ext-web ownership store is shared across adapter instances.

The current Go host already consumes ext's `res/system` and `res/web/host`,
but the five named helper packages are not yet thin adapters. The published
ext platform SDKs and their versions must be selected deliberately by the
embedding application; a Go module cache path is not a distributable Swift,
Java, or .NET dependency.
