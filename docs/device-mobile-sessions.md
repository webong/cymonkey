# Mobile target sessions

Cymonkey now includes a native-to-Board media bridge for iOS and Android.
The source adapters send bounded JPEG/PCM frames over an authenticated WebSocket;
Board converts them to VP8/PCMU and exposes the same WebRTC peer contract used by
the browser viewer. Mobile viewers use `pkg/device-viewer` independently.

This implementation has compiler/build evidence, not live-device certification.
The embedding app owns OS consent, visible capture UI, foreground-service or
broadcast-extension packaging, signing, and credentials obtained from its host.
No system-wide mobile input injection is exposed.

## Host-controlled lifecycle

1. The trusted host calls `POST /v1/device-targets` with its operator bearer:

   ```json
   {"name":"Approved phone","video":true,"audio":false,"microphone":false,"playback":false,"ttlSeconds":300}
   ```

   Every media direction is independently enabled. At least one must be true.
   `iceServers` and `relayOnly` optionally configure the Board WebRTC side. Use
   short-lived TURN credentials; supply corresponding viewer ICE configuration.
2. The response contains `adapterId`, `deviceId: "peer"`, `path`, a scoped
   `token`, `expiresAt` and the enabled `media` flags. Deliver only this target
   credential to the phone. Connect to `path` as `wss://...` with
   `Authorization: Bearer <token>`. No credentials belong in query strings.
3. The phone obtains fresh local consent and starts the enabled capture sources.
   Only one socket may attach to that target. Failed/closed attachments retire
   it; reconnect requires a new host registration and local capture lifecycle.
4. The host opens `/v1/device-sessions` on the returned adapter/device with
   `webrtc.signal`, `webrtc.poll`, and the desired subset of registered media
   capabilities. Issue its viewer token as usual. Without a granted viewer,
   incoming capture is discarded instead of stored.
5. `DELETE /v1/device-targets/{adapterId}` with the **operator** credential stops
   the producer connection and all media sources. Target expiry and server
   shutdown do the same. Mobile `onStop` must close native capture/playback.
   Closing a viewer alone does not revoke the independently authorized producer.

Limits: 16 registered targets per operator, lifetime 1–3600 seconds (default 300),
1 GiB of ingress per target, two queued video frames, at most 50 PCM packets per
active audio source, and 12 queued playback packets. Each source permits one
viewer attachment at a time. Target streaming requires ffmpeg with VP8 support.

## Binary wire format

Each WebSocket message is one kind byte followed by data:

| Kind | Direction | Data |
| --- | --- | --- |
| 1 | Phone → Board | One JPEG, at most 4 MiB, dimensions at most 4096 each and 8 megapixels total |
| 2 | Phone → Board | System/app audio, PCM16 little-endian stereo 48 kHz |
| 3 | Phone → Board | Microphone, same PCM format |
| 4 | Board → Phone | Playback, same PCM format |

PCM messages contain complete stereo frames, 4–3840 bytes (at most 20 ms).
Video is paced to at most 10 frames/second; stale queued frames are dropped.
Audio backpressure closes the producer rather than retaining unbounded audio.
No JSON actions, filesystem paths, shell commands, or arbitrary input events are
accepted on the producer socket. The target token cannot open viewer sessions.

## iOS integration

`DeviceTargetTransport` implements `DeviceCaptureSink`. Initialize it from the
host response and grant flags, then call `connect`. Pass it to `DeviceCapture`
for app capture, or return it from a `DeviceBroadcastHandler.makeSink` override
inside a host-owned Broadcast Upload Extension. Override `permits` from the same
host grant flags; the default broadcast filter permits video only.

The transport scales JPEG frames to fit 1280 pixels, preserves ReplayKit
orientation metadata, converts independent audio sources to PCM16 stereo 48 kHz,
and bounds pending sends. If playback is granted, configure AVAudioSession and
pass a serialized callback to `DevicePlayback.write`.

Always dispatch the `onStop` callback to the main actor to stop `DeviceCapture`
and close playback. In a broadcast extension finish the broadcast and release
its sink. A network failure must stop local capture, not leave it recording.
Present RPSystemBroadcastPickerView from the containing app for cross-app
sharing, with the appropriate extension and microphone usage configuration.

## Android integration

The library declares OkHttp 4.12.0. The host owns its `OkHttpClient`; construct
`DeviceTargetTransport` with the scoped URL/token, grant flags and callbacks.
`onReady` and playback callbacks run on network threads: post capture startup to
the main thread, after fresh MediaProjection consent and a visible foreground
service. Construct `DeviceTargetCapture` with the granted projection and flags.
It owns the ImageReader, scales frames, captures enabled audio, and converts
mono capture to the bridge's stereo format.

In `onStop`, call `DeviceTargetCapture.close` and close playback. Capture teardown
stops projection and its owned audio recorders. For incoming playback, forward
to `DevicePlayback` through a bounded worker queue; handle partial writes before
accepting another packet. Do not perform blocking playback in the WebSocket
listener. The host must declare INTERNET plus the foreground-service and audio
permissions described in [device platforms](device-platforms.md).

## Transport and input options

Desktop WebRTC configurations can set `"audioCodec":"opus"` (default `pcmu`).
Opus sources encode PCM through libopus-enabled ffmpeg; playback decodes Opus
into the authorized PCM channel. Outgoing Opus requests in-band FEC, while the
Board receive path uses bounded reordering and inserted Opus silence for gaps.
It does not implement native FEC decoding or retransmission of lost audio.
Mobile target bridge peers currently use PCMU audio.

For desktop pointer control, register a `pointer` device with an absolute helper
path and fixed `source` (a macOS display ID or Windows `primary`). Bind it to a
WebRTC peer's `pointer` target and explicitly list `pointerCapabilities` from
`pointer.move`, `pointer.click`, `pointer.scroll`, `pointer.down`, `pointer.up`,
`pointer.release`. The public device ID is
`pointer`. Keep the pointer's approved display consistent with the video source.
Positions are normalized x/y in [0,1], clicks have an explicit left/right/middle
button, and scroll deltas are bounded to ±100 lines. Held-button sessions require move/down/up/release grants together. A down action
selects one explicit button, moves drag it, and up releases it. `pointer.release`
takes `{}` and releases the session-owned button at its current position. macOS requires Accessibility permission; Windows
input stays within the caller's interactive desktop privilege.

## Evidence still required

Live iOS/Android capture, both desktop native audio paths, direct and forced
TURN routes, denial/revocation, rotation/backgrounding, and signed host builds
remain unverified. The target registration/streaming schema is included in
`src/protocol/device/v1alpha1/device.schema.json`. Compiler success alone does
not certify the media pipeline or permission behavior on a real device.

## Reference hosts

Build and run the [Cymonkey reference hosts](../examples/device-hosts/README.md)
to exercise the reusable libraries before product adoption. They include local
signing configuration, consent/lifecycle examples and a pending acceptance record.
Signing identities, approved hardware and TURN access must be supplied by the
validation operator. Product-specific integration remains with the consuming app.

## Desktop drag lifetime

Board retains one pointer connection per WebRTC peer, opens it lazily on first
input, and releases it on session close, expiry or revocation. Each pointer
provider permits one owner session at a time. Configure one provider per physical
pointer; creating duplicate providers for the same OS desktop does not create
independent pointers. Concurrent input from a person or another process can still
interact with the same OS pointer.

The `pointer-session` native helper tracks one held button. EOF, invalid input,
failed commands and a five-second idle deadline release it. Browser clients send
position updates once per second while holding, and release on blur, visibility
loss, pointer cancellation or lost capture. The deadline is a fallback when the
producer disappears; it is not proof of immediate network-loss detection. A
WebRTC peer with the down grant closes on transport disconnection or input-channel
closure; reconnect using a fresh session. Native permission loss, OS failure or
forced termination of the helper itself can prevent OS event delivery.

Custom RTC integrations use `Options.OpenInput` for a session-owned handler and
cleanup; `BoardInputSession` supplies this for Board connections. The old
per-action `BoardInput` path is suitable only for atomic input. Upgrade the native
helper alongside Board to enable the new process protocol.

Live acceptance still requires dragging a harmless test window on Mac and Windows,
then releasing normally, outside the viewer, on blur, on expiry/revoke, on network
loss and on parent exit. Confirm that another session cannot inject while the
first owns the provider. No new live input checks have been performed here.

Example pointer grant/configuration capability set:

```json
["pointer.move", "pointer.down", "pointer.up", "pointer.release"]
```

On one authorized connection, invoke `pointer.down` with
`{"x":0.2,"y":0.3,"button":"left"}`, then `pointer.move` with
`{"x":0.6,"y":0.5}`, then `pointer.up` with
`{"x":0.6,"y":0.5,"button":"left"}`. Never open a separate connection for
those actions. Add click/scroll grants only when those independent actions are
needed; attempting either while a button is held ends the native helper session.
