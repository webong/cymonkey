# Board device implementation ownership

Cymonkey owns the reusable device and I/O implementations through its independent
Board library. Board covers physical and virtual devices, local or remote.
Xallet will consume these libraries and runtime artifacts as a host.

This extraction takes implementation and existing tests from Xallet revision
`d281a1e84dcd08cee4583dc121a92152cdf6035e`. The source Xallet checkout is retained
for its later integration; it has not yet switched to these implementations.
New device-mechanics development belongs here.

## Public packages

All imports below are relative to the standalone `board` Go module at `lib/board`.
These low-level SDKs are also wrapped by `board/devices` providers, with
`board/rtc` for WebRTC and `board/config` for host-owned inventory loading.
[Device sessions](device-sessions.md) documents registration, grants, duplex I/O,
operator routes, expiry, events, and platform limits. Hosts calling low-level
SDKs directly must enforce authorization themselves.
The existing registry-backed mounted-drive and macOS keyboard providers remain
available through Board's provider/session contract.

| Board import | Public entry points | Xallet source |
| --- | --- | --- |
| `board/displayaction` | `New`, `Capture`, `Input`, `RecordingArgs`, `StartRecording`, `StopRecording` | `sdk/displayaction` |
| `board/displayprobe` | `New`, `Observe`, `ResolveX11Socket` | `sdk/displayprobe` |
| `board/permissionprobe` | `New`, `Open` | `sdk/permissionprobe` |
| `board/filesystemprobe` | `VolumeCapacityBytes`, `DirectoryUsageBytes` | `sdk/filesystemprobe` |
| `board/volumearchive` | `Export`, `Restore`, `FileDigest` | `sdk/volumearchive` |
| `board/networkprobe` | `New`, `Observe`, `NewRequest`, `NewTCPRequest` | `sdk/networkprobe` |
| `board/audio` | `ParsePulseDevices` with caller-selected device IDs | PulseAudio parsing in `internal/runtime/display_oci_session.go` |
| `board/vncgateway` | `Server.Handler` | `interfaces/vncgateway` |
| `board/rtc` | `Message`, `Forwarder`, `New`, Board media adapters | `internal/display/webrtc.go` contract and `internal/runtime/display_webrtc.go` forwarding, plus new Pion media transport |

Display observations use Board's own `Observation`, `Endpoint`, and
`RecordingCapability` types. Network probes use Board's own `Request` and
`Response`, with no Xallet protocol, request ID, or operation envelope. The host
maps results to its own API/IPC envelope. Board imports no Xallet package.

```go
readiness := displayprobe.New(displayprobe.Config{}).Observe()
// Only after the host authorizes this particular endpoint:
health, err := networkprobe.New(networkprobe.Config{}).Observe(
    ctx, networkprobe.NewTCPRequest("127.0.0.1:5999"),
)
```

The network client rejects redirects to a different authority, bounds redirect
chains, and closes response bodies without draining content. A custom injected
client/dialer must maintain the host's destination authorization and time limits.

## Runtime deployment

Board owns reusable device mechanics and the authenticated gateway handler. It
does not own X11, Wayland, PulseAudio, VNC, RDP, or SPICE process deployment.
Those recipes stay with Xallet or another deploying host, which chooses
machines, containers, private networks, socket mounts, credentials, and
lifecycle. Cymonkey can host the gateway handler through
`cymy device gateway serve` after the host supplies a private upstream.

## Current limits

- Native display actions preserve the existing desktop-wide command behavior.
  The provider wrapper enforces Board grants and requires explicit desktop scope,
  but does not select an individual window. Prefer
  the existing `macoskeyboard` provider for input bound to a specific process.
- Native capture invokes OS tools. macOS uses `screencapture`, Windows uses
  PowerShell/.NET, and Linux uses `grim` or ImageMagick. Input uses platform
  command tools; Linux currently accepts only the key operation.
- Recording uses FFmpeg, with inherited platform source selection (including
  the macOS `1:none` source). The host approves the output path and recording
  duration. Stop only recordings started through the returned process handle.
- Display probes inspect environment, sockets, and installed tools. A ready
  result is not a live permission/capture check. Permission onboarding opens
  settings; it does not grant permission or complete a PipeWire portal session.
- Audio devices are virtual. The null microphone does not capture a physical
  microphone. Device observation checks listings, not live media delivery.
- Archive operations require host-controlled source/destination directories and
  deployment quotas. Provider transfers bound compressed/decompressed sizes and
  use pinned directory handles. Restore requires an empty destination and rejects
  traversal and unsupported entries; concurrent host mutation is unsupported,
  and restoration is not an atomic whole-directory transaction.
- `Connection.OpenChannel` adds bounded continuous/duplex streams beside finite
  reads. Provider registration is explicit, using factories or trusted configuration;
  availability checks and lifecycle events are scoped to authorized sessions.
- The gateway relays bytes without decoding VNC/RDP/SPICE. The separate Pion
  WebRTC provider carries VP8 video, PCMU audio, and independently granted
  keyboard input. It does not transcode a VNC/RDP/SPICE endpoint into WebRTC.

## Xallet adoption

1. Select a Board source revision or published module version. The current module
   name is `board`; local development can require `board v0.0.0` and replace it
   with the checked-out `lib/board` directory. Board also requires the adjacent
   `providerplugin` module; a consuming module must supply that replacement too.
   A published module identity/version must be established before remote Go
   dependency resolution can replace these local mappings.
2. Change Xallet SDK/Organizer adapters to call Board packages. Map Board
   observations into Xallet protocol responses; keep request IDs and Xallet's
   operation envelope in Xallet.
3. Supply Xallet's authorized command runner to native actions and permission
   helpers, and its authorized client/dialer to network probes. Keep Host Broker
   admission, identity, grants, audit, and artifact metadata at the host boundary.
4. Keep Xallet's runtime images and service manifests in Xallet. Wire their
   approved endpoints and device identifiers into the Board providers used by
   the host.
5. Retire Xallet's duplicate SDK code and image recipes after its callers and
   deployment manifests consume the Board implementation and pass integration
   checks. The existing gateway accepts legacy `XALLET_*` environment aliases.

Xallet's Entity/Computer graph, peer identity, routing policy, approvals, workload
placement, and product HTTP/ACP interfaces continue to be host responsibilities.

## Validation

The extracted source tests, audio device-selection checks, network bounds tests,
and Board dependency-isolation test pass with `GOWORK=off go test ./...` from
`lib/board`. The module also builds for Linux/amd64 and Windows/amd64 with cgo
disabled. All extracted runtime entrypoints pass `sh -n`.

Native actions use fake command runners. Local Pion peers exercise VP8 video, bidirectional
audio, keyboard/playback grant denial, and ICE restart; operator checks cover persistent
sessions, WebSocket upload, restore, expiry, and cleanup. Real desktop capture,
physical audio, cross-network TURN, and container startup are deployment checks;
images have not been built or published.
