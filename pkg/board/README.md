# Board

Board holds the device implementations used by Cymonkey, covering physical and
virtual devices in local and remote environments. The contract for discovering
devices, opening a host-approved session and invoking bounded capabilities, with
its grant gate, lives in the device kit (`lib/jangolova/devicekit`). A Board provider
binds that contract to a particular operating system, device class, or
transport.

Board runs as an ext package built from `cmd/provider`; see [device sessions](../../docs/device-sessions.md#board-device-provider-package). It ships a read-only mounted-drive provider and a macOS keyboard provider.
The host registers each provider and authorizes each attachment. Board does not
discover, mount, or manage physical devices. There is no hotplug event source.

Board also owns the embeddable `vncgateway` HTTP handler. It authenticates a
WebSocket connection and relays bytes between a
viewer and one caller-configured private VNC, RDP, or SPICE endpoint. Board
does not create or manage the display server or target lifecycle, and does not
decode the display protocol. Cymonkey hosts the handler through
`cymy device gateway serve`. The deploying host owns any container, network,
and process configuration around that command.

## Reusable device implementations

Board owns the following SDK packages extracted from Xallet:

- `displayaction`: native desktop capture, keyboard input, and FFmpeg recording.
- `displayprobe`: desktop, X11/Wayland, and recording prerequisite observations.
- `permissionprobe`: OS capture-permission settings navigation.
- `filesystemprobe`: capacity and usage of a caller-selected volume.
- `volumearchive`: directory archive export, restore, and digest calculation.
- `networkprobe`: HTTP(S) and TCP endpoint health observations.
- `audio`: virtual audio device observation from PulseAudio listings.

The deploying host supplies any virtual X11, Wayland, or PulseAudio runtime,
along with placement, authorization, private networking, and device/socket
access.

Use `board/devices` for grant-enforced desktop, audio, storage, and fixed TCP
providers. `board/rtc` adds WebRTC signaling and media backed by those providers.
`board/config` loads an explicit host inventory; Cymonkey accepts it through
`CYMONKEY_DEVICE_CONFIG`. Connections support finite content, bounded duplex
channels, and cursor-based lifecycle events. See [device sessions](../../docs/device-sessions.md)
for configuration, capability grants, operator routes, and platform limits.
The low-level SDKs remain available for hosts that enforce authorization themselves.

Audio channels in `board/devices` accept `encoding`, `sampleRate`, and
`channels`. PCM is signed 16-bit little endian; omitting rate/channels preserves
48 kHz stereo. Hosts can request mono 24 kHz with
`{"encoding":"pcm","sampleRate":24000,"channels":1}` for a compatible realtime
model adapter. PCM allows 8–192 kHz and 1–8 channels; PCMU remains 8 kHz mono.
The caller opens the granted channel with explicit byte/time limits and owns
capture/playback lifetime. No model connection opens a device automatically.

## First workflow: import photos from a USB drive

1. A registered drive provider reports the USB drive and its capabilities.
2. The host approves `drive.list` and `drive.read` for a selected resource
   root. The drive provider resolves that opaque root and confines all listing
   and reading to it.
3. The macOS keyboard provider can separately receive a grant for
   `keyboard.press` or `keyboard.type` for an explicitly selected process.
4. The host coordinates file selection and app interaction through
   `devicekit/host` or its own Board registry.

The first capability names are `keyboard.press`, `keyboard.type`, `drive.list`,
and `drive.read`. Providers may define more names for other devices. A grant
names one provider, one device, and a subset of that device's advertised
capabilities. Drive grants also require opaque root IDs. Each drive action
must name one granted root; the provider must confine any path inside the
action input beneath that root, including aliases and links. Board cannot
resolve provider-specific resources itself.

## The mounted-drive provider

`NewMountedDriveProvider` takes a host-approved root. It never discovers,
mounts, or unmounts a volume, and it never writes. `RootID` is the opaque
handle an action must name; the host filesystem path is deliberately not part
of the action contract and is not echoed back into results.

An action path must be relative, contain no NUL byte, and stay within the root
after `..` normalization. Each session opens the root with `os.Root`; listing
and reading use that handle, including when a path component changes during an
action. Relative symlinks that resolve inside the root work. Links that leave
the root are refused; a listing withholds inaccessible entries and increments
`skipped`. The provider also pins the original directory and compares its
identity with the host path on every new action. A removed or replaced mount
returns `ErrDriveUnavailable` and needs a new host approval.

`MaxListEntries` bounds the result, `MaxScanEntries` bounds directory entries
inspected, and `MaxReadBytes` bounds each content stream. A partial listing is
marked `truncated`; a host command that promises a complete file must refuse a
partial stream.
`os.Root` confinement does not prevent traversal into a mount nested inside
the approved directory. A host that needs a single-volume policy must approve
a root without nested mounts or enforce that policy separately.

## Results and streaming

`Result.Output` is small structured JSON, which keeps it safe to log, compare,
and forward. File bytes do not belong in it. A provider that implements
`StreamingSession` delivers contents through `Connection.Stream`, which returns
a `Content`: a forward-only reader with a fixed `Size`, a `Truncated` flag, and
a `Close`. The bound lives in the stream, so a caller cannot read past it by
ignoring the size, and an action's `maxBytes` can only tighten the provider's
bound. `Connection.Stream` runs the same grant gate as `Connection.Invoke`, so
an ungranted capability or resource fails before any provider code runs, and
closing a session closes the streams it handed out.

`drive.read` through `Invoke` returns `ErrStreamRequired` rather than
inlining content into a result. `board.ReadAll` exists for a host that genuinely
needs bytes in memory and requires it to state its own ceiling.

## Extend Board

Implement `Provider` and `Session`, then register the provider explicitly with
`NewRegistry`. Each device advertises capability names and JSON input schemas.
The provider lists only devices it can access, validates each action's input,
applies the supplied grant, and releases its own attachment on `Close`. Board
checks that requested capabilities were advertised and gates each invocation
against the grant. The host supplies authorization before opening a session.

```go
registry, err := board.NewRegistry(myKeyboardProvider, myDriveProvider)
if err != nil { /* handle registration error */ }

connection, err := registry.Open(ctx, board.OpenRequest{
    ProviderID: "local-drives",
    DeviceID:   "usb-1",
    Grant: board.Grant{
        Capabilities: []board.Capability{board.DriveList, board.DriveRead},
        ResourceIDs:  []string{"approved-photos-root"},
    },
})
if err != nil { /* handle unavailable or denied device */ }
defer connection.Close(ctx)

// Contents come from a bounded stream, never from a result.
content, err := connection.Stream(ctx, board.Action{
    Capability: board.DriveRead,
    ResourceID: "approved-photos-root",
    Input:      json.RawMessage(`{"path":"DCIM/100CANON/IMG_0001.JPG"}`),
})
if err != nil { /* handle a refused or unavailable path */ }
defer content.Close()
```

Registering the mounted-drive provider takes a host-approved root:

```go
drive, err := board.NewMountedDriveProvider("mounted-drive-photos", board.MountedDriveOptions{
    Root:           "/Volumes/NOAA",
    Name:           "Field Camera USB",
    RootID:         "photos",
    MaxReadBytes:   16 << 20,
    MaxListEntries: 500,
    MaxScanEntries: 2000,
})
if err != nil { /* the approved root is missing or unusable */ }

registry, err := board.NewRegistry(drive)
```

## macOS keyboard provider

`macoskeyboard.New` requires a positive target PID chosen by the caller and
macOS Accessibility permission for the calling process. It records the
target process start identity and rejects actions if that process exits or its
PID is reused. `keyboard.press` accepts named keys or modifiers plus an ANSI
letter, such as `Command+O`. `keyboard.type` accepts bounded Unicode text
without control characters. Its result `{"submitted":true}` means the events
were posted; the target application may still ignore them. This provider needs
macOS with cgo; other builds return `ErrUnsupported`.

```go
keyboard, err := macoskeyboard.New(macoskeyboard.Options{
    ProviderID: "keyboard-import", PID: targetPID,
})
if err != nil { /* handle permission or unavailable target */ }
registry, err := board.NewRegistry(keyboard)
```

Board does not ship a CLI. Cymonkey exposes these operations through
`cymy device keyboard-press` and `cymy device keyboard-type`. `devicekit/host`
also accepts caller-owned providers with `host.NewRegistered`, and its
`Attach` method requires an explicit `Authorizer` for every grant.

## Lifecycle and errors

Providers are registered explicitly. List, open, invoke or stream, then close
the connection. Close a mounted-drive provider after its connections to
release its pinned directory handle. On `ErrDriveUnavailable` or
`macoskeyboard.ErrTarget`, the host should select and approve a current target
before creating a new provider. `ErrNotGranted` and `ErrClosed` let callers
distinguish grant and lifecycle failures. `Connection.Events` reports bounded
metadata events, and configured providers with a health grant report readiness
changes. Device registration remains host-controlled; Board does not scan for
unregistered devices.

Board is its own Go module (`module board`) and imports neither
Cymonkey nor Jangolova nor Blockade; a dependency-graph test enforces that. Run
`go test ./...` from this directory.

## Capture and clipboard SDKs

`board/capture` provides bounded PNG processing and read-only sampling over a
caller-supplied source: digest/metadata manifests, changed-region detection,
heartbeat retention, crop, element-box annotation and contact sheets.
`SampleVideo` accepts explicit offsets and a borrowed decoder. `Bridge` borrows
a native capture caller; `Around` captures evidence around one caller-selected
operation. Storage, codec and target ownership stay with the host.

`board/clipboard.NewProvider` adapts a host-supplied clipboard backend to explicit
Board capabilities/resources. Read returns finite content; describe returns
metadata. Write/clear require the observed generation. Restoration is another
explicit write. Atomicity against other processes depends on the backend; macOS
uses change-count checks and cannot guarantee an atomic cross-process operation.
Board action JSON remains limited to 64 KiB.

`board/audio.WAV` and `ReadWAV` encode/decode finite PCM16 containers without
opening a microphone/player, resampling, or selecting an output destination.

See [feature extraction](../../docs/tachikoma-peekaboo-extraction.md) for native
byte limits and independent clipboard/viewer grants.
