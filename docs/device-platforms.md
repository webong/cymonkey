# Native device media and cross-platform viewer

## Implementation status

These changes add reusable native media primitives and a scoped browser viewer.
They do **not** complete end-to-end support for all four operating systems.
Desktop audio now has a `nativeAudio` Board provider, session-owned helper
executables, and ffmpeg format conversion connected to PCMU or Opus WebRTC
source/playback adapters. Mobile capture now has scoped authenticated WebSocket
transport, JPEG/PCM conversion and registered Board WebRTC targets. The legacy `audio` provider remains PulseAudio;
use `nativeAudio` explicitly for the desktop native helper path.

| Platform | New target primitives | Viewer | Validation |
| --- | --- | --- | --- |
| macOS 13+ | ScreenCaptureKit selected display and system audio; AVAudioEngine microphone; bounded PCM playback | Shared WebRTC browser client | Native audio executable built against macOS 15.2 SDK; no live capture |
| Windows | WASAPI selected-endpoint microphone, loopback capture and bounded PCM playback library; existing desktop video/input | Shared WebRTC browser client | C# helper built with zero warnings; self-contained win-x64 publish produced; no Windows execution |
| iOS 15+ | ReplayKit app capture, broadcast-extension base, app/system audio and microphone samples; bounded PCM playback | Shared WebRTC browser client | New Swift files type-checked against iOS 18.2 SDK; no device run |
| Android 26+ | MediaProjection encoder surface, microphone capture; playback capture on API 29+; bounded AudioTrack playback | Shared WebRTC browser client | New Kotlin files compiled against Android 36 SDK; no device run |

## Host integration

Native libraries deliberately do not authenticate by themselves. The host must
bind the selected source to a device session and enforce separate display,
system-audio, microphone and playback grants before starting each source. Close
all owned resources on expiry, revocation, transport failure and user stop.
Keep capture callbacks short; synchronously encode or copy into a bounded queue
and drop old video frames under backpressure. Do not retain unbounded buffers.

- macOS: `DeviceMedia.displays()` enumerates eligible displays; `start` takes an
  approved display ID. `DeviceMediaSink` receives screen/system audio sample
  buffers and microphone PCM. Grant Screen Recording and Microphone permissions
  to the embedding application; supply its microphone usage description.
  `DevicePlayback` accepts PCM16 little-endian stereo 48 kHz in packets up to
  20 ms with at most 12 queued packets. Serialize playback calls. Explicitly stop
  capture when its host session ends.
- Windows: reference `lib/windows-helper/Media/Cymonkey.Windows.Media.csproj`.
  `DeviceAudio.Endpoints()` lists endpoints; select an ID locally before opening
  `Capture` or `Playback`. Capture delivers its actual `WaveFormat`, requiring
  normalization before encoding. Playback requires PCM16 stereo 48 kHz, with a
  250 ms bounded queue. Call lifecycle operations from the host session thread,
  never dispose capture from its sample callback. NAudio is pinned to 2.2.1.
- iOS: `DeviceCapture` supplies app capture. For cross-app sharing, the host must
  add a Broadcast Upload Extension whose principal class subclasses
  `DeviceBroadcastHandler`, implement its authenticated `makeSink` and `permits` grant filter, and present
  `RPSystemBroadcastPickerView`. Include ReplayKit broadcast extension metadata,
  microphone usage text and configure the app's audio session for playback.
  Stopping/revoking the session must stop capture/broadcast and close the sink.
  No system-wide input injection is advertised.
- Android: the Activity requests a **fresh** MediaProjection consent result each
  session, then starts a host-owned visible foreground service before creating
  the projection. Construct `DeviceProjection` on the main thread, supply the
  encoder surface to `start`, and call `resize` for source-size changes. The
  service owns notification and stop controls. `openAudio` requires RECORD_AUDIO
  runtime permission; microphone capture additionally needs the microphone
  foreground-service type on applicable releases. Manifest declarations belong
  to the embedding application: FOREGROUND_SERVICE,
  FOREGROUND_SERVICE_MEDIA_PROJECTION, RECORD_AUDIO, and when used,
  FOREGROUND_SERVICE_MICROPHONE. The library adds none globally. Read returned
  audio recorders from a worker; `close` releases them. Playback capture respects
  source-app capture restrictions. `DevicePlayback.write` returns partial writes
  instead of building an unbounded queue. Arbitrary global input is not exposed.

The consent/lifecycle design follows [Android MediaProjection documentation](https://developer.android.com/media/grow/media-projection)
and [Apple ReplayKit capture documentation](https://developer.apple.com/documentation/replaykit/rpscreenrecorder/startcapture(handler:completionhandler:)).
The Windows wrapper uses [NAudio's WASAPI interfaces](https://github.com/naudio/NAudio/tree/v2.2.1).

## Remaining before claiming full platform support

1. Signed mobile host apps, iOS broadcast extension and Android foreground service
   integration, including consent UI and playback workers. The delivered adapters
   are reusable libraries, not installable apps.
2. Live viewer/target combinations across all four OSes, direct and forced TURN,
   checking permission denial, rotation, backgrounding, revoke, expiry and
   interrupted connectivity.
3. Native receiver Opus FEC decoding/retransmission, if required: the current
   receiver reorders packets and inserts silence; outbound encoding enables FEC
   for compatible browser receivers. The mobile bridge currently uses PCMU.
4. Live validation of desktop held-button/drag cleanup and platform-specific
   app-owned mobile input. Desktop move, click, scroll and session-owned holds
   are implemented with separate grants; mobile global input is not exposed.

The Board module, operator and macOS native helper build. The Windows helper
build and self-contained publish succeeded. Mobile Swift type-checking and Kotlin
compilation succeeded; these are not signed app builds or live acceptance.
No tests were run in this implementation increment.

See [mobile sessions, Opus and pointer integration](device-mobile-sessions.md)
for the registration protocol, lifecycle responsibilities and configuration.

## Native desktop audio sessions

Build the macOS executable:

```sh
swift build --package-path lib/macos-helper --product cymonkey-device-audio
```

On a Windows .NET 8 build host:

```sh
dotnet publish lib/windows-helper/Media/Cymonkey.Windows.Media.csproj -c Release -r win-x64 --self-contained true
```

Install the executable at a host-owned absolute path and provide `ffmpeg` on the
operator's PATH. Copy [the native audio configuration](../etc/device/native-audio.example.json)
and replace the helper path and approved source IDs. macOS system audio requires
a display ID from `cymonkey-device-audio describe ""`; microphone/playback use
`default`. Windows accepts endpoint IDs from the same command, or `default`.
The describe command does not request permissions or start capture. On macOS the
host must grant Screen Recording/Microphone permission to the executable's app
identity; signed application packaging remains a host deployment responsibility.

Set `CYMONKEY_DEVICE_CONFIG` to the configuration's absolute path. The native
adapter exposes device `audio`, and the example peer exposes device `peer`.
Grant `webrtc.signal` and `webrtc.poll`, plus only the desired `audio.capture`,
`audio.microphone` or `audio.playback` capabilities when opening the peer session.
The viewer credential continues to authorize just that session's signaling.
`audio.describe` reports selected sources and current helper availability without
opening media. OS permission/device changes may still make a later open fail.

Each native channel owns two processes: the helper and a format converter.
Capture is a streaming WAV from the helper, normalized to PCM16 stereo 48 kHz or
PCMU mono 8 kHz. Playback is converted to PCM16 stereo 48 kHz for the helper.
OS pipes bound intermediate storage. Closing the channel, session, or peer kills
both owned processes; it does not stop the caller's desktop or other sessions.
The configured executable and source IDs cannot be changed by remote actions.

PCMU receive playout uses a 60 ms startup delay, a 200 ms timestamp window and
20 ms output frames. Duplicate and late samples are discarded, missing samples
become silence, and a prolonged gap resets the timestamp baseline. This conceals
loss; it does not recover lost speech or provide Opus FEC.

`keyboard.press` accepts `Enter`, `Tab`, `Space`, `Escape`, `Backspace`, `Delete`,
`Home`, `End`, `PageUp`, `PageDown`, `ArrowLeft/Right/Up/Down`, `F1`–`F12`, single
characters and `Plus`. Prefix with `Control+`, `Alt+`, `Shift+`, or macOS `Meta+`
(Command). Windows Meta shortcuts are explicitly unsupported by this backend.
Presses are atomic; no held-key state survives focus loss. `keyboard.type`
escapes Windows SendKeys syntax so typed punctuation cannot become shortcuts.

## Reference hosts

Build and run the [Cymonkey reference hosts](../etc/device-hosts/README.md)
to exercise the reusable libraries before product adoption. They include local
signing configuration, consent/lifecycle examples and a pending acceptance record.
Signing identities, approved hardware and TURN access must be supplied by the
validation operator. Product-specific integration remains with the consuming app.
