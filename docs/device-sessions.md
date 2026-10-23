# Cymonkey device sessions

Cymonkey exposes host approved input and output devices through the
`cymonkey.device/v1alpha1` contract. Devices may represent a keyboard, display,
audio endpoint, storage volume, network endpoint, or WebRTC peer.

Every action is limited by an explicit grant. Discovering a display does not
grant capture or input. Audio capture, microphone access, and playback are
separate capabilities. Storage actions require an approved resource handle.

## Configure host devices

Set `CYMONKEY_DEVICE_CONFIG` to an absolute path to a host owned JSON file. No
file means no native devices are added implicitly. Installed
`device.provider@v1` plugins are loaded from the shared plugin store.

```json
{
  "devices": [
    {
      "id": "screen",
      "desktop": {
        "scope": "desktop",
        "environment": {"DISPLAY": ":99"},
        "capture": true,
        "video": true,
        "input": true,
        "recording": true,
        "recordingDirectory": "/srv/cymonkey/recordings",
        "recordingSeconds": 60
      }
    },
    {
      "id": "sound",
      "audio": {
        "socket": "/run/cymonkey-audio/pulse/native",
        "outputDevice": "cymonkey_output",
        "inputDevice": "cymonkey_microphone",
        "systemCapture": true,
        "microphone": true,
        "playback": true
      }
    },
    {
      "id": "files",
      "storage": {
        "path": "/srv/cymonkey/transfers",
        "resourceId": "transfer-root",
        "allowRestore": true,
        "maxTransferBytes": 134217728,
        "maxExtractedBytes": 1073741824
      }
    },
    {"id": "vnc", "endpoint": {"address": "127.0.0.1:5999"}}
  ]
}
```

The host creates approved directories, supplies runtime sockets, and grants
the OS permissions needed for capture or input. Native helpers may require
`ffmpeg`, ImageMagick `import`, `xdotool`, `grim`, `wtype`, `pactl`, or `pacat`,
depending on the enabled platform features.

## Direct commands

List or use devices through Cymy:

```sh
cymy device adapter-devices
cymy device adapter-invoke \
  --adapter storage-plugin --device volume \
  --capability storage.inspect --resource transfer-root --input '{}'
```

## Board device provider package

Board's device implementations reach the host as an ext package that serves
`device.provider`. Build and install it once:

```sh
go -C pkg/board build -o "$PWD/board-provider" ./cmd/provider
cymy plugins manifest --type device --name board --version 1.0.0 --command ./board-provider
cymy plugins install --manifest ./plugin.json
```

The host passes the package non-secret settings with each list and open:
`inventory` (the absolute path in `CYMONKEY_DEVICE_CONFIG`) and, for the
keyboard commands, `keyboardPid`. The package builds its own providers from them
and reports each device under the provider ID that owns it, so a device keeps
its inventory ID. `cymy device keyboard-press` and `keyboard-type` use the same
package; without it installed they report that no device plugin serves the
keyboard.

Mounted drive and macOS keyboard commands require the caller to name the exact
root or process:

```sh
cymy device devices --root photos=/srv/photos --json
cymy device drive-list --root photos=/srv/photos --drive photos --path .
cymy device keyboard-press --pid 12345 --key Command+O
printf 'caption' | cymy device keyboard-type --pid 12345
```

Expose one caller-owned private VNC, RDP, or SPICE TCP endpoint through an
authenticated WebSocket gateway:

```sh
export CYMONKEY_DEVICE_GATEWAY_TOKEN='replace-with-a-random-secret'
cymy device gateway serve --bind 127.0.0.1:6080 \
  --upstream 127.0.0.1:5999 \
  --token-env CYMONKEY_DEVICE_GATEWAY_TOKEN
```

The command relays protocol bytes. It does not create a display server, choose
a target, decode frames, or own the upstream lifecycle.

## Operator routes

Every route requires `Authorization: Bearer $CYMONKEY_OPERATOR_TOKEN`.

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/v1/devices` | List public device descriptors |
| `POST` | `/v1/device-actions` | Run one bounded action with a one capability grant |
| `POST` | `/v1/device-streams` | Read one finite stream, up to 16 MiB |
| `POST` | `/v1/device-sessions` | Open a persistent granted connection |
| `POST` | `/v1/device-sessions/{id}/actions` | Invoke an action in the session |
| `POST` | `/v1/device-sessions/{id}/streams` | Read a finite session stream, up to 1 GiB |
| `GET` | `/v1/device-sessions/{id}/channels` | Open bounded duplex WebSocket I/O |
| `GET` | `/v1/device-sessions/{id}/events?after=0` | Read lifecycle events |
| `DELETE` | `/v1/device-sessions/{id}` | Close the session and owned activity |

An open request uses public `adapterId` and `deviceId` fields:

```json
{
  "open": {
    "adapterId": "viewer-plugin",
    "deviceId": "peer",
    "grant": {
      "capabilities": ["webrtc.signal", "webrtc.poll", "display.video"]
    }
  },
  "ttlSeconds": 300
}
```

Sessions default to five minutes, accept up to one hour, and expire
automatically. The operator permits 64 active sessions. Closing a session also
closes its streams, channels, recordings, and temporary transfer artifacts.

## Duplex channels

The first WebSocket text message declares an action and byte budget:

```json
{
  "action": {
    "capability": "storage.upload",
    "resourceId": "transfer-root",
    "input": {}
  },
  "readBytes": 0,
  "writeBytes": 134217728,
  "durationSeconds": 60
}
```

The server replies `{"opened":true}`. Binary messages then carry content. Send
`{"close":true}` and wait for `{"closed":true}` before committing a storage
restore. Byte budgets are at most 1 TiB per direction, messages are limited to
64 KiB, and the channel cannot outlive its session.

Browser WebSocket clients cannot set the bearer header. A trusted host proxy
or SDK client must establish these operator channels. Tokens never belong in a
query string.

## Operational limits

- Capture and input are desktop scoped unless a plugin provides a narrower
  resource model.
- Storage access stays beneath its approved root and rejects traversal,
  escaping links, and unsupported archive entries.
- Recording output is bounded by configured duration and size.
- WebRTC has no public STUN or TURN default; the host supplies ICE servers and
  credentials.
- Availability probes report readiness. They do not prove that capture or
  input permissions will remain valid for a later action.

## Compatibility and validation

Session creation responses include `apiVersion: "cymonkey.device/v1alpha1"`.
The request envelopes are specified in
[`device.schema.json`](../src/protocol/device/v1alpha1/device.schema.json).
Clients must use the public `adapterId` field and ignore additional response
fields. Session deletion revokes open channels immediately, including idle
write-only channels.

See [adoption validation](device-validation.md) for automated checks, required
live evidence, and generation of a local candidate with artifact checksums.
A successful build does not certify live-device permissions or remote networks.


## Cross-platform remote access

The [device roadmap](roadmap.md#device-access-across-macos-windows-ios-and-android)
covers macOS, Windows, iOS and Android. A scoped browser viewer and native media
primitives, desktop audio wiring and authenticated mobile target registration
are implemented. Signed mobile host integration and live acceptance remain pending. See [platform implementation status](device-platforms.md) for
exact coverage. Use the explicit `nativeAudio` provider for native Mac/Windows audio; the
legacy `audio` provider still requires PulseAudio. See the platform guide for
helper builds and source selection. See [mobile sessions](device-mobile-sessions.md)
for target registration, Opus and separately granted pointer controls.
