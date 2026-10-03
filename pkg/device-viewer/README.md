# Cymonkey device viewer

ES module and minimal browser UI for macOS, Windows, iOS and Android browsers
that implement WebRTC. This is implementation coverage, not a verified device
compatibility matrix. Serve these files from the **same HTTPS origin** as the
operator via the host's trusted reverse proxy (localhost is suitable for development).
The operator does not serve these static files automatically.

## Attach

1. The trusted host opens `/v1/device-sessions` with a selected WebRTC adapter,
   `webrtc.signal`, `webrtc.poll` and only the desired media/input grants.
2. With its operator bearer, the host calls
   `POST /v1/device-sessions/{sessionId}/viewer`.
3. Deliver the response's `path` and `token` privately to the viewer. Never give
   the viewer the operator credential or put any credential in a URL.
4. Import `DeviceViewer` from `viewer.js`, pass `{url:path, token, video}` and
   call `connect({microphone:false, keyboard:false})`. The included UI allows
   these choices explicitly. Host grants remain authoritative.
5. `close()` sends a best-effort peer bye, stops microphone tracks and closes
   the peer. The host uses DELETE on the session for authoritative revocation.

A viewer credential authorizes only signaling/polling for one session. It cannot
open another session, enumerate devices, access storage, or invoke arbitrary
actions. Issuing again rotates HTTP access; existing media requires session
revocation to stop. Credentials expire with the session (at most one hour).
No cross-origin CORS access is enabled by the bridge.

`iceServers` and `relayOnly` constructor options configure the viewer's ICE path;
the host must also configure its target peer. Supply short-lived TURN credentials
through the trusted host, never in committed examples. `state` events report
connection changes/errors. `restart()` restarts ICE on the same session; an
expired session requires new host authorization and a new viewer instance.

`type(text)` and `press(key)` send atomic actions. The UI currently exposes text
only because native named-key/modifier mappings are not yet normalized. There
are no held keys and no global event listeners that forward typing from other
fields. Pointer control is not implemented. Microphone upload negotiates PCMU;
Opus transport is still pending. Use Play when mobile autoplay blocks audio.

See [native adapter integration](../../docs/device-platforms.md) for target
adapters, host responsibilities and the unfinished transport work.
