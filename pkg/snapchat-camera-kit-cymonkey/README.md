# Snapchat Camera Kit Cymonkey package

This is an optional `render/browser-dom` sandbox augmentation package. It integrates
Snap Camera Kit Web without adding Snap's SDK, a camera permission, or Snap
credentials to Cymonkey Browser Extension itself.

The package owns a canvas and its Camera Kit session. It exposes only its
declared Cymonkey actions:

- `camera-kit.overlay.mount`
- `camera-kit.session.describe`
- `camera-kit.lens.apply`
- `camera-kit.lens.remove`
- `camera-kit.camera.stop`
- `camera-kit.overlay.unmount`

`camera-kit.overlay.mount` creates an owned overlay containing a **Start
camera** button. Only that button asks the configured media provider for a
camera stream; an agent cannot open the camera through `act` alone. The target owner supplies the Snap
API token when composing the package and must have Camera Kit Web access and
Lens IDs from Snap.

## Sandbox deployment

Camera Kit Web downloads its Lens renderer as WebAssembly from Snap. It cannot
execute in the ordinary MV3 extension or content-script context. Cymonkey
therefore runs this package inside its extension sandbox page: a separate
extension origin with no WebExtension APIs and a narrowly composed Camera Kit
Content Security Policy.

Build the composed Chrome/Edge artifact with:

```sh
npm run build:camera-kit-browser-package
```

That build puts this package at
`augmentations/snapchat-camera-kit/sandbox.js`; `pkg/browser-ext` still has no
Snap dependency. The caller mounts the generic sandbox and then sends normal
Cymonkey requests through it:

```json
{
  "name": "augmentation.mount",
  "input": {
    "augmentationId": "my-camera-kit",
    "id": "camera",
    "package": "snapchat-camera-kit",
    "target": {"tabId": 42},
    "permissions": ["camera"],
    "configuration": {"apiToken": "supplied-by-target-owner"}
  }
}
```

The first camera-permission mount returns a short-lived `approvalId`. The user
reviews it in the Cymonkey extension popup and chooses **Allow once**. The
caller then retries the identical input with that `approvalId`; successful
mounting consumes it.

`cymonkey-engine.call` uses `delivery: "sandbox"`, `augmentationId`, and
`sandboxId: "camera"` to call the package. The page does not receive the
token, port, or any extension APIs.

## Consent

The user must click the owned **Start camera** button before capture begins. A
target-owned standalone runtime uses `getUserMedia` directly. The browser
extension uses Cymonkey's generic offscreen media broker and a local WebRTC
track because an opaque extension sandbox cannot call `getUserMedia`, and an
arbitrary target site's Permissions Policy must not decide whether an approved
extension package can acquire media. An agent may apply or remove an approved
Lens only after that explicit camera session has started.
