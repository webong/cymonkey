# Board reference hosts

These are small Cymonkey integration fixtures. They exercise the same native
libraries a consuming app links; they contain no Xallet product logic, account
system, deployment control or operator credential. Signing is supplied locally
by the person running the fixtures. Do not commit provisioning profiles or keys.

## Android

Open `android` in Android Studio (JDK 17, Android SDK 36, Gradle 8.14.3), or run:

```sh
gradle -p examples/device-hosts/android :app:assembleDebug
```

The debug APK uses a local development signing identity. Install it on an
approved Android 10+ device. The app depends on the repository's helper module;
production consumers can depend on its AAR instead. The reference app requires
WSS with a trusted certificate and does not disable TLS verification.

Paste a newly issued target WebSocket URL and scoped token. Video is always on;
select only audio directions granted at registration. Tap Consent and start,
accept audio permission if requested, and choose the OS capture scope. Each
attempt obtains a fresh projection token. The service is non-exported and
non-sticky, starts its notification before projection use, and stops on network
failure, OS revocation or the Stop action. Playback uses a 12-packet worker queue
and handles partial native writes. Rotation currently permits letterboxing.
Audio playback capture remains subject to the source application's restrictions.

## iOS

The XcodeGen specification builds an app plus Broadcast Upload Extension,
both linking the local `CymonkeyIOSHelper` Swift package. Install XcodeGen,
then generate and open the project:

```sh
cd examples/device-hosts/ios
xcodegen generate
open BoardReference.xcodeproj
```

Set your development team, a unique `BOARD_BUNDLE_PREFIX`, and a registered
`BOARD_APP_GROUP` shared by both targets. Generated plists/entitlements/projects
are ignored; `project.yml` is the source. Build the BoardReference scheme for an
approved iPhone/iPad. Automatic signing requires your own Apple identity.
Unsigned compilation is not permission or device evidence.

For app-only capture, paste WSS endpoint/token, enable the granted directions,
and tap Start app capture. Leaving the app stops this capture. Playback is
supported in this mode and runs on a serialized queue. Stop and remote closure
release capture, playback and the audio session.

For cross-app capture, disable playback, stage a fresh credential, then use the
system broadcast picker. Staging stores the scoped credential in a protected,
backup-excluded App Group file for up to two minutes while the app is running;
the extension validates the deadline and consumes/deletes it once. Suspended
apps cannot guarantee timely deletion, so the server's target expiry remains
mandatory. App launch and Clear also remove the staged file. Never stage an
operator credential. The extension filters audio directions independently of the
system microphone switch. End an active broadcast using the OS indicator, or
revoke the target at the operator. The app's Stop button cannot stop a separate
broadcast extension. Broadcast playback is deliberately not implemented here.

## Session setup and viewers

Follow [mobile target registration](../../docs/device-mobile-sessions.md).
The trusted harness registers video plus independently enabled audio flags,
passes only the producer URL/token to the phone, and opens a viewer session on
the returned adapter. Use `pkg/device-viewer` in Safari/Chrome on the viewer
phone or desktop; no second native viewer implementation is needed.

Use an operator served over trusted HTTPS, with the viewer on the same origin.
No automatic resource provisioning, global audio routing or permission database
changes are part of these fixtures. Re-register after failure or expiry.

## Validation procedure

1. Prepare dedicated test content (animation plus a known tone) on an approved
   target. Record OS/browser versions, artifact hashes and independent grants.
2. Run each relevant viewer/target pairing with default ICE policy. Record two
   connection-evidence snapshots several seconds apart: decoded frames/bytes
   must increase and the selected route must be recorded. A relay route in this
   run does not establish direct connectivity.
3. Repeat on separate networks with host-supplied short-lived TURN credentials.
   Set `relayOnly` on target registration and check Require TURN relay in the
   viewer. Supply the viewer's ICE servers via Network validation. Confirm a
   selected relay candidate; an empty route is inconclusive, never a pass.
4. Check visual movement, each separately granted audio direction, permission
   denial, target microphone revocation, expiry, explicit target deletion,
   producer disconnect, viewer disconnect, network loss and fresh reconnection.
   No-grant runs must not produce the corresponding media. Closing only a viewer
   leaves the independently registered producer alive until stop/expiry.
5. Exercise Android rotation, backgrounding and OS stop; iOS app background stop,
   broadcast pause/resume and system stop. Verify native indicators stop and
   owned converter processes exit after producer revocation.
6. Fill `acceptance-template.json`; duplicate a run for each pairing/route.
   Leave unexecuted checks pending. Attach only sanitized counters and observations,
   never tokens, IP addresses, captured screens or audio. Mobile global input is
   unsupported; desktop pointer/keyboard checks require dedicated desktop targets.

The viewer's Show connection evidence exports selected candidate *types*, codec
names and counters, excluding credentials, SDP and candidate addresses. Copy it
while connected; this is observational evidence, not an automatic pass verdict.

See [Android projection lifecycle](https://developer.android.com/media/grow/media-projection)
and [Apple's broadcast picker](https://developer.apple.com/documentation/replaykit/rpsystembroadcastpickerview).

## Current build evidence (2026-10-08)

- Android: `:app:assembleDebug` succeeded with Gradle 8.14.3 and SDK 36.
  APK: `android/app/build/outputs/apk/debug/app-debug.apk` (generated, ignored).
  SHA-256: `08efe543b98de112f1e44f1ef7bde8bb95a0df378c866c99a149a9f28ea31031`.
  One warning remains for the backwards-compatible Parcelable extra accessor.
- iOS: helper sources, app and extension type-checked against iOS 18.2 SDK with
  iOS 15 deployment target; helper also checked with application-extension API
  restrictions. XcodeGen is not installed on this host, so project generation,
  full app-bundle build and signing have not been exercised here.
- Viewer JavaScript syntax and the acceptance JSON parse successfully.
- No devices were installed to, no live capture was started, and no TURN run or
  automated test suite was executed. All live acceptance records remain pending.
