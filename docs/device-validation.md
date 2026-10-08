# Device adoption validation

The public contract remains `cymonkey.device/v1alpha1`. This is an alpha
compatibility boundary, not a claim of hardware certification. The reference
request schemas are in `src/protocol/device/v1alpha1/device.schema.json`; the
corresponding Go envelopes are in `src/contracts/device.go`.

Requests use `adapterId`, never an internal library provider ID field. Unknown
request fields are rejected. Session creation returns `apiVersion`, a random
`sessionId`, and `expiresAt`. Clients should ignore additional response fields.
Session TTL uses seconds; omitted or zero means 300, with a maximum of 3600.
Channel byte budgets are integers; zero disables that direction. Capability
schemas and provider limits still apply after envelope validation.

## Automated evidence

Run from the repository root, with Go dependencies already available:

```sh
(cd lib/board && GOWORK=off go test -race ./...)
GOWORK=off go test -race ./src -run 'TestOperatorDevice|TestDeviceCommandUsesPublicCLI'
```

These checks cover local peer media, grants, transfer lifecycle, expiry and
revocation. They require loopback TCP/UDP listeners. They do not prove live
hardware permissions or NAT traversal. Deleting a session must immediately
close idle write-only WebSockets as well as active readers; clients must not
have to wait for the original stream deadline after revocation.

## Required live evidence

Use dedicated, explicitly approved targets. Record platform/tool versions,
Cymonkey artifact checksum, capabilities, time bounds, and pass/fail evidence.
Do not include credentials, desktop contents, or recorded audio in ordinary logs.

| Environment | Check | Acceptance |
| --- | --- | --- |
| macOS/Windows/X11 | Capture and bounded recording on a selected test display | Decodable output; correct target; stop/expiry removes owned processes |
| Approved input target | A harmless key in a dedicated test app | Reaches the intended target; ungranted input never reaches it |
| OS permission boundary | Denied permission, granted permission, then revoked permission | Clear failure on denial/revocation; session closes without retaining access |
| PulseAudio target | Known test tone capture and playback, microphone separately granted | Expected media; independent grants; socket disconnect ends owned streams |
| Linux Wayland | Portal selection, cancellation, stream close/revocation | Pending implementation and a real Wayland/PipeWire host; do not label supported yet |
| Two separate networks | ICE direct route and relay-only route with host-supplied TURN | Confirm selected ICE candidate pair uses the required route, then restart and reconnect |
| Storage/gateway | Temporary root and private test upstream | Transfer fidelity; rejected traversal; bounded data; clean disconnect |

Do not simulate permission approval by editing OS permission databases. Real
permission changes use the platform's normal consent mechanism. Test data and
windows must be chosen before input/capture checks. Do not provision cloud
resources or change global audio routing as a side effect of validation.

## Local release candidate

```sh
python3 scripts/prepare-device-release.py --output /tmp/cymonkey-device-candidate
```

The script builds the current host target offline, packages the public schema
and these guides, and writes a manifest with the binary SHA-256, Go build info,
source revision, source fingerprint, and whether the source is modified. It
refuses to overwrite an existing output directory and aborts if the source
changes while building. It does not tag, commit, upload, or publish anything.

A dirty checkout produces a clearly marked local candidate. Its Git revision
alone is insufficient to reproduce it. For a release, use `--require-clean` from
the intended clean revision, attach validation evidence for that artifact, and
publish through the project's approved release process. Xallet should pin the
artifact checksum and public contract version. Source modification status must
not be omitted when sharing a working-tree candidate.

## Open capability work

- Continuous Wayland/PipeWire capture and portal lifetime management.
- Live acceptance of implemented macOS/Windows native audio paths.
- Live codec interoperability and loss behavior for implemented Opus/PCMU buffering.
- Live acceptance of the scoped viewer, media/input controls and reconnect behavior.
- Mac → Mac, Mac → Windows, Windows → Mac, and Windows → Windows
  acceptance runs, both local-network and forced TURN relay.
- Explicit platform-specific native capture-source selection.
- Live permission/hardware and two-network validation described above.

Deployment images, service manifests, and workload placement remain owned by
Xallet or another deploying host. Removing duplicate device implementation code
does not authorize deleting those deployment recipes.

## Cross-platform completion criteria

The detailed work items and acceptance criteria are tracked in the
[device access roadmap](roadmap.md#device-access-across-macos-windows-ios-and-android).
A native capture implementation or a successful local WebRTC test alone does
not complete a platform pairing. Each pairing needs evidence from the actual
viewer and target OS, with independent system-audio/microphone/playback grants,
correct keyboard mappings, permission denial/revocation, and session cleanup.

The viewer is a Cymonkey client of the device contract. Board owns reusable
platform and transport mechanics. A browser viewer must receive narrowly scoped
session access through a trusted bridge, not the operator's inventory-wide token.

## Mobile reference hosts

[Reference apps and acceptance procedure](../examples/device-hosts/README.md) provide
an iOS app/broadcast extension and an Android projection service using the helper
libraries. They belong to Cymonkey as integration fixtures. Consuming products own
their own app packaging, signing, policy and deployment. The evidence template
starts pending; builds and type-checks do not mark any device run as passed.
