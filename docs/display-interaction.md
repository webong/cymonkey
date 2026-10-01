# Display interaction

Cymonkey provides a provider-neutral display contract for targets that do not
expose a richer semantic API, including Linux GUI applications in containers
or VMs, headless desktops, and legacy software.

The `display-interaction` adapter connects to caller-owned display endpoints
such as VNC/RFB, WebRTC video/data channels, or Wayland input/capture relays.
It translates high-level requests into frame captures and coordinate-based
pointer or keyboard events.

---

## Ownership Boundary

```text
External agent / application
       │
       ▼  (Semantic methods: display.capture, pointer.click, keyboard.type)
Cymonkey `display-interaction` adapter
       │
       ├── RFB / VNC Client Protocol
       ├── WebRTC Video/Data Channel Client Protocol
       └── Wayland / X11 Input & Frame Client Protocol
       │
       ▼  (Network / Loopback Endpoint: vnc://, webrtc://, rfb://)
Caller-Owned Display Target (Owned by Operator / Xallet / Custom Source)
       ├── Custom x11vnc / wayvnc server
       ├── WebRTC display stream server
       ├── QEMU / KVM virtual machine display
       └── Container GUI workload
```

* **Target owner (caller / Xallet / operator) owns:** The display server process (`x11vnc`, `wayvnc`, QEMU VNC, WebRTC server), frame buffers, window manager, operating system permissions, and network endpoints. `cymy device gateway serve` can expose one private VNC/RDP/SPICE TCP stream over authenticated WebSocket; it does not create or manage that endpoint.
* **Cymonkey owns:** The `display-interaction` adapter, protocol parsing, coordinate translation, input action dispatch, and semantic capabilities (`display.capture`, `pointer.click`, `keyboard.type`).

Disconnecting Cymonkey releases its VNC/WebRTC client connection without stopping the underlying display server or terminating application windows.

---

## Supported Endpoints & Protocols

A caller supplies a standard `cymonkey.display-target/v1alpha1` descriptor with target kind `display`, `linux-application`, `native`, `vm`, or `container`:

```json
{
  "apiVersion": "cymonkey.display-target/v1alpha1",
  "targetId": "linux-gui-container-1",
  "kind": "display",
  "endpoints": [
    {
      "name": "vnc-display",
      "protocol": "vnc",
      "url": "vnc://127.0.0.1:5900",
      "credentialRef": "vnc-password-ref"
    }
  ]
}
```

Supported protocol schemes for automatic engine selection (`engine.adapter: "auto"`):
* `vnc` / `rfb` → RFB (Remote Frame Buffer) display protocol.
* `webrtc` → WebRTC video frame capture and data channel input protocol.
* `wayland-rfb` → Wayland-native remote capture and virtual input protocol.

---

## Semantic Capabilities & Actions

The `display-interaction` engine exposes the standard five bridge methods (`hello`, `capabilities`, `describe`, `act`, `events`, `health`).

### Offered Capabilities

* `display.describe` — Returns screen dimensions, color depth, coordinate space boundaries, and focus state.
* `display.capture` — Captures a visual snapshot of the screen/viewport (PNG/JPEG encoded base64).
* `pointer.move` — Moves mouse cursor to `(x, y)` coordinates.
* `pointer.click` — Performs mouse click at `(x, y)` (supports `left`, `right`, `middle` buttons, and click count).
* `pointer.drag` — Performs mouse drag from `(startX, startY)` to `(endX, endY)`.
* `pointer.scroll` — Performs mouse wheel scroll at `(x, y)` with `deltaX` and `deltaY`.
* `keyboard.type` — Types a sequence of text characters into active focused window (supports `sensitive: true` flag for audit redaction).
* `keyboard.press` — Presses specific key or key combination (e.g. `Control+C`, `Enter`, `Tab`, `Escape`).

---

## Policy & Security Bounds

The `display-interaction` engine supports policy restrictions to protect sensitive UI regions and prevent unsafe system shortcuts:

* **`allowedBounds`**: Enforces strict `(minX, minY, maxX, maxY)` coordinate bounds for pointer actions. Any move, click, drag, or scroll outside these bounds is rejected with a policy error.
* **`maxTextLength`**: Restricts the maximum string length permitted per `keyboard.type` action.
* **`blockedKeys`**: Rejects forbidden key shortcuts (e.g. `Control+Alt+Delete`, `Super`).
* **`redactSensitiveInput`**: Automatically redacts typed text from audit events and logs when `sensitive: true` is set or policy redaction is active (`***REDACTED***`).
* **Audit Events**: Emits structured session events (`display.action.invoked`, `display.action.denied`, `display.action.failed`) for full accountability.

---

## Example Usage

Connect to a caller-owned VNC server:

```bash
cymy display connect \
  --adapter display-interaction \
  --target-kind display \
  --endpoint vnc=vnc://127.0.0.1:5900
```

Or invoke through the display HTTP API:

```http
POST /v1/display-instances/display-one/call
Authorization: Bearer ...
Content-Type: application/json

{
  "method": "act",
  "params": {
    "name": "pointer.click",
    "input": {
      "x": 450,
      "y": 320,
      "button": "left"
    }
  }
}
```
