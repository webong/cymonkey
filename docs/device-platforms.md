# Native device media and cross-platform viewer

## Implementation status

These changes add reusable native media primitives and a scoped browser viewer.
They do **not** complete end-to-end support for all four operating systems.
In particular, native media callbacks still require encoding/conversion and
attachment to the shared Board WebRTC transport; the current configured audio
provider remains PulseAudio. Do not advertise the new primitives as connected
Board devices until that wiring is complete.

| Platform | New target primitives | Viewer | Validation |
| --- | --- | --- | --- |
| macOS 13+ | ScreenCaptureKit selected display and system audio; AVAudioEngine microphone; bounded PCM playback | Shared WebRTC browser client | New Swift files type-checked against macOS 15.2 SDK; no live capture |
| Windows | WASAPI selected-endpoint microphone, loopback capture and bounded PCM playback library; existing desktop video/input | Shared WebRTC browser client | New C# library not compiled; no .NET SDK available here |
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
- Windows: reference `pkg/windows-helper/Media/Cymonkey.Windows.Media.csproj`.
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

1. Native helper lifecycle and authenticated media transport wiring into Board
   discovery, grants, format conversion and WebRTC source/sink adapters.
2. Mobile host application/extension/service integration, codec pipelines and
   signed device builds. These adapters are libraries, not installable apps.
3. Shared Opus negotiation, bounded jitter handling and loss recovery.
4. Named-key/modifier mappings; separately authorized pointer controls and
   platform-specific app-owned input handlers.
5. Source selection and permission state in the public capability descriptors.
6. Live viewer/target combinations across all four OSes, direct and forced TURN,
   checking permission denial, rotation, backgrounding, revoke, expiry and
   interrupted connectivity. No live acceptance tests were run in this change.

The new operator code builds. Swift validation is type-checking of the new files,
not an entire app build. Existing tests were not run or extended in this change.
