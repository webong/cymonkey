# Design: mobile targets as a guest

Status: implemented. It was the last place the `cymy` host linked a guest
module (`src/operator_device_targets.go` imported `board/rtc`). The root module
no longer requires `board`, `console` or the endpoints module and there is no
`go.work`. Two details differ from the text below: the package serves the device
operations under `mobile.target` itself (rather than a second `device.provider`
contract) so that device discovery does not launch it, and `events` polling is
only done for packages whose manifest declares it.

## What the host does today

A native app (the mobile reference hosts in `etc/device-hosts`) streams its screen,
system audio and microphone to Cymonkey, and plays audio sent back. A viewer (a
browser or another agent) watches and talks over WebRTC.

1. The operator creates a target: `POST /v1/device-targets` with the operator
   token and options (ICE servers, relay only, name, which media to allow, a
   lifetime). The host builds a `rtc.MobileMedia` (bounded media queues) and a
   `rtc.Provider` (a Board device provider backed by pion WebRTC) and returns an
   adapter ID, a one-time credential and a path.
2. The app connects once to `GET /v1/device-targets/{id}` as a WebSocket with that
   credential and pushes binary frames: one kind byte plus a payload (1 JPEG
   video, 2 PCM system audio, 3 PCM microphone). It receives kind 4 (PCM playback)
   frames. The host validates every frame (kind granted, size bounds, JPEG
   dimensions, a one-gigabyte ceiling) and calls `MobileMedia.Push`.
3. A viewer opens a normal device session on the target's adapter ID. Its actions
   are `webrtc.signal`, `webrtc.poll` and `device.health`; pion sends the media
   it received from the app to the viewer and the viewer's microphone back.

So there are two halves. The viewer half is an ordinary Board provider whose
operations are all `Invoke` calls. The app half is a long-lived byte stream at
video rates (megabytes per second, tens of messages per second).

## Constraints

- The host must not import `board/rtc` or pion.
- The mobile app's protocol and the operator API stay exactly as they are.
- JSON lines cannot carry the media: JPEG frames of up to 4 MiB, 20 ms audio
  chunks 50 times a second per stream. The data plane needs a real byte stream.
- A `device.provider` process serves one session and is started per list and per
  open. A mobile target has state that must exist before the viewer arrives (the
  app attaches first) and must live for the target's lifetime.
- ext v0.6.0 now provides the pieces: reverse calls, progress, and declared
  loopback endpoints with grants (`contract/endpoint`).

## Proposal

### One long-lived guest process per target

`board/cmd/mobile` is a new executable. The host starts one process for each
target when it is created and keeps it until the target is removed or expires.
The process hosts the pion peer and the `MobileMedia`, so ffmpeg and the WebRTC
stack run in the guest with ext's scrubbed environment.

It serves two contracts:

| Contract | Operations | Purpose |
| --- | --- | --- |
| `mobile.target` v1 | `start`, `stop` | Control. `start` takes the target options, builds the media and provider, starts the ingest listener and returns the declared endpoint. |
| `device.provider` v1 | `list`, `open`, `invoke`, `stream.*`, `close`, and a new `events` | The viewer half, unchanged except for `events` below. |

`start` carries what `POST /v1/device-targets` accepts today: ICE servers
(as plain `{urls, username, credential}` data, so the host needs no pion type),
relay only, name, media grants and lifetime. The guest validates them, checks that
ffmpeg is available when a media grant needs it, and refuses otherwise; the host
maps that refusal to the existing 503.

### Data plane: a declared endpoint and a relay in the host

The guest cannot listen where the phone can reach it (a declared endpoint is
loopback only, by design), and the phone's credential and rate limits are the
operator's business. So the host keeps terminating the app's WebSocket and
relays it:

1. `start` returns an `endpoint.Endpoint` (`ws`, `127.0.0.1:port`, a secret path,
   a token, `expiresAt` equal to the target's expiry). The host admits it with
   `endpoint.Admit(process, endpoint, policy{ws})`; the grant ends with the
   process.
2. When the app attaches, the host authenticates it as today (token hash, single
   attachment, expiry), then dials the guest's endpoint with
   `grant.AuthorizationHeader()` and pumps frames in both directions.
3. The host keeps the checks it has now, in the pump: kind granted, size bounds,
   JPEG dimensions, the byte ceiling. The guest's `MobileMedia.Push` repeats the
   size checks, so a bug in the relay cannot push oversize frames into pion.
4. Playback frames (kind 4) flow the other way through the same pump.

This costs one extra copy of each frame through the host on loopback. It keeps
the app-facing surface, the credential and the policy exactly where they are.

### Viewer half: the existing contract plus `events`

The viewer's operations already fit `device.provider`. The host registers a
provider for each live target that forwards `list`, `open`, `invoke` and `close`
to that target's one long-lived process instead of launching a new one. It needs:

- a plugin adapter variant bound to an existing process, with an admission policy
  suited to a long-lived process (`list` any time, `open` once per viewer session,
  `close` then a new `open` allowed) instead of the per-process sequence policy;
- an `events` operation, additive to `device.provider` v1, served when a session
  can report lifecycle events (the pion peer already does: connected, failed,
  closed). The host polls it as the display plugin adapter polls its events and
  exposes it as the session's event source, so `GET .../events` keeps working. We
  keep to additive-only changes within v1.

`webrtc.poll` is the signaling mailbox and needs no change.

### Lifecycle

- Create: validate, start the process, call `start`, admit the endpoint, register
  the provider, return the same response as today.
- Attach: unchanged checks, then open the relay. A second attach is refused.
- Viewer session: opens and closes against the live process; a new viewer can
  open after one closes while the target lives.
- Expire or delete: close the relay, call `stop`, close the process. If the guest
  process dies, the grant ends, the relay closes, and the target is removed.
- Host shutdown: close every target's process.
- The limit of 16 concurrent targets stays; it is also the cap on processes.

### Installation

A new package, `mobile-target`:

```sh
go build -o mobile ./pkg/board/cmd/mobile
cymy plugins manifest --type mobile-target --name mobile --version 1.0.0 --command ./mobile
cymy plugins install --manifest ./plugin.json
```

Without it `POST /v1/device-targets` answers 503 with a clear message. That is a
behaviour change, like the other services.

## Security notes

- The endpoint (address, path, token) is a secret; ext keeps it out of errors and
  logs, and so must the host. The app never sees it.
- The app's credential still never reaches the guest. The guest sees only the
  relay's connection.
- The guest runs with ext's scrubbed environment; ffmpeg is found through `PATH`.
- Frames from the app are validated in the host before they reach the guest, and
  again by size in the guest.

## Alternatives considered

- Leave `rtc` in the host as the one exception. Simple, but it keeps pion and
  ffmpeg handling in the host process and the root module requiring `board`.
- A process per viewer session (the existing device model). The app attaches
  before any viewer exists, so the target state would need a second home.
- The app connects straight to the guest. A declared endpoint is loopback only,
  and it would bypass the operator's credential and limits.
- Carry frames in JSON-line calls. Far too heavy and lossy at video rates.

## Steps

1. Add `events` to `device.provider` in `jangolova/devicekit` (guest serve helper
   and the plugin adapter's event source), with tests.
2. Add a plugin adapter variant for a live process and its admission policy.
3. Build `board/cmd/mobile`: the `mobile.target` contract, the ingest listener
   (one WebSocket, bearer token), and the viewer provider on `device.provider`.
4. Move `createMobileTarget` and `handleMobileTarget` onto it: start, admit,
   relay, lifecycle. Replace the pion ICE server type with plain data.
5. Tests: the guest on its own; the relay with a fake app, including oversize
   and invalid frames; expiry and process death; and an end-to-end run with
   ffmpeg and a WebRTC peer if both are available (skipped otherwise).
6. Remove `board/rtc` from the host, empty `guest_imports.txt`, delete the three
   guest `require` and `replace` lines from the root `go.mod` and the `go.work`
   entries, and add the installation steps to `docs/device-mobile-sessions.md`.

## Questions for review

1. Is a relay in the host acceptable? It is one more copy on loopback and some
   latency; the alternative is giving up the operator's control of the app's
   connection.
2. A separate `mobile-target` package, or serve `mobile.target` from the existing
   Board provider package so there is one install? I lean to separate: the two
   have different process lifetimes and admission policies.
3. Is an additive `events` operation on `device.provider` v1 fine?
4. Should the feedback on declared endpoints go back to ext after this? It will be
   their first real use outside a probe.
