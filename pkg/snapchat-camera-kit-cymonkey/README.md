# Snapchat Camera Kit Cymonkey package

This is an optional `render/browser-dom` augmentation package. It integrates
Snap Camera Kit Web without adding Snap's SDK, a camera permission, or Snap
credentials to Cymonkey's core.

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

## Browser-host integration

Camera Kit Web downloads its Lens renderer as WebAssembly from Snap. A
consuming extension can bundle this package in its own sandbox page with a
reviewed Content Security Policy and camera consent flow. Cymonkey provides
the package and browser-adapter library; it does not ship the sandbox page or
a composed Chrome/Edge extension.

Build the package with:

```sh
npm --prefix pkg/snapchat-camera-kit-cymonkey run build
```

The integrating extension decides where to include the output and how to
mount it. A compatible host can send normal Cymonkey requests:

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

The integrating extension must obtain user consent before granting camera
access. It may use a short-lived approval ID tied to the exact mount request.

`cymonkey-engine.call` uses `delivery: "sandbox"`, `augmentationId`, and
`sandboxId: "camera"` to call the package. The page does not receive the
token, port, or any extension APIs.

## Consent

The user must click the owned **Start camera** button before capture begins. A
target-owned standalone runtime uses `getUserMedia` directly. An integrating
extension with an opaque sandbox must supply its own approved media broker and
private track transport. An agent may apply or remove an approved Lens only
after that explicit camera session has started.
