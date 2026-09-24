# Browser augmentation packages

`pkg/browser-ext` is Cymonkey's browser platform: it owns browser
permissions, target selection, authentication, authorization, audit, storage,
network rules, and packaged-script injection. It is not a catalogue of
JavaScript libraries.

Three.js, Snapchat Camera Kit, and other Jangolova runtime libraries are
optional augmentation packages. Cymonkey selects and mounts one for a target;
the package owns the runtime-native semantics. It is then shipped as reviewed
extension code below:

```text
augmentations/<package-id>/
├── manifest.json
├── content.js
├── sandbox.js
└── assets/...
```

The extension accepts only files below that package directory. It never
downloads and executes an arbitrary npm package or remote JavaScript at
runtime. That preserves WebExtension policy, makes the package reviewable, and
keeps Cymonkey's entry boundary separate from the Jangolova library boundary.

Every product artifact also carries `augmentations/registry.json`. A package
is mountable only when it appears in that reviewed registry, its canonical
manifest is present, its delivery supports the current browser, and every
requested iframe permission is declared by the manifest. Merely placing an
unlisted JavaScript file in the artifact does not make it executable through
the Cymonkey control plane.

The extension popup is also a standalone package manager. It lists only the
reviewed registry, accepts an ephemeral JSON configuration, targets the active
tab, and runs the package's declared `launch` action after mounting. The
configuration remains in popup memory only; Cymonkey does not persist tokens
or copy them into approval or audit records. This path is owned by Cymonkey
and works without Xallet or an outbound control connection.

If the selected HTTP(S) tab predates extension installation or reload,
Cymonkey attaches its packaged MAIN-world bridge and isolated runtime before
retrying the request. Protected browser settings, extension pages, and other
non-web schemes are rejected with an explicit target error.

This restriction matters for SDKs that download executable code themselves.
For example, Snap Camera Kit Web downloads its Lens renderer as WebAssembly.
It cannot run in an ordinary MV3 extension or content-script context, but it
can run in Cymonkey's explicitly declared extension sandbox. The sandbox is
still shipped and selected as reviewed product code; only the SDK's documented
runtime resources are allowed by that sandbox page's CSP.

## Package contract

A package first has a normal Cymonkey augmentation manifest: it declares the
target, requested permissions, and lifecycle. Its implementation uses
`script.execute` for a one-time action or `script.register` for a persistent
matching content script.

If the package exposes a richer runtime, its reviewed `content.js` registers a
factory in Cymonkey's private isolated world. The generic content host mounts
one factory result per `augmentationId` and routes the private channel
`cymonkey.jangolova.augmentation-runtime` to it. The package receives only a
standard Cymonkey request (`hello`, `capabilities`, `describe`, `act`, or
`events`) and returns only its declared semantic result.

Both ordinary and sandbox deliveries mount through the same semantic action:

```json
{
  "name": "augmentation.mount",
  "input": {
    "augmentationId": "example-overlay",
    "package": "example",
    "target": {"tabId": 42},
    "configuration": {}
  }
}
```

The reviewed package manifest selects `augmentation-package`/`content.js` or
`sandbox`/`sandbox.js` for the current browser. Callers do not select a
library-specific extension action.

Cymonkey routes that request with:

```json
{
  "type": "CYMONKEY_EXTENSION_CALL",
  "method": "cymonkey-engine.call",
  "params": {
    "augmentationId": "example-overlay",
    "delivery": "augmentation-package",
    "target": {"tabId": 42},
    "request": {"id": "1", "method": "capabilities"}
  }
}
```

The control policy authorizes this call by caller, capability, tab/origin, and
`augmentationId`; it also audits it. The browser extension does not interpret
the package's library objects or invent library-specific actions.

## Sandbox packages

Some packages need a browser capability that an ordinary content script cannot
provide safely—for example, a camera-owning WebAssembly renderer. A sandbox
package is loaded by the extension's `runtime-sandbox.html`, which has no
`chrome.*` or `browser.*` APIs. Its only control connection is a nonce-bound
`MessageChannel` to the content-script-owned overlay.

The package mounts through the generic semantic action:

```json
{
  "name": "augmentation.mount",
  "input": {
    "augmentationId": "camera-overlay",
    "id": "camera",
    "package": "snapchat-camera-kit",
    "target": {"tabId": 42},
    "permissions": ["camera"],
    "configuration": {"apiToken": "target-owner-provided"}
  }
}
```

After mounting, `cymonkey-engine.call` uses `delivery: "sandbox"`, the
augmentation ID, and the sandbox ID to send the same Cymonkey request to that
package. The extension policy authorizes the mount and every call before the
page-side relay sees them. No page script can access the port, the package
configuration, or extension APIs.

An optional package `launch` entry names the first declared capability that a
standalone package manager should invoke after the sandbox has connected. It
does not add a new extension API and cannot name a capability absent from that
package's manifest.

When the mount requests a sensitive permission such as `camera`, the first
call returns `status: "approval-required"` with a short-lived approval ID. The
extension badge indicates a pending request. The user opens Cymonkey, reviews
the package, permission, and target origin, and chooses **Allow once** or
**Deny**. An approved caller retries the identical mount with `approvalId`;
the approval is consumed and cannot be reused for another package, origin,
tab, or augmentation. Package configuration, including API tokens, is never
stored in the approval record.

When the user initiates the mount from Cymonkey's own popup, approving it
automatically retries that exact in-memory request. Calls from an agent control
plane keep the same explicit retry behavior. Xallet Spook and authenticated
WebSocket callers are optional integrations; neither is required for the
standalone popup path.

Sandboxes are currently available in composed Chrome and Edge artifacts.
Firefox and Safari may still mount ordinary reviewed augmentation packages;
they simply exclude package manifests that only provide a sandbox delivery.

## Examples

| Package | Owns | May expose |
| --- | --- | --- |
| Three.js overlay | Its canvas, scene, camera, and objects | Explicitly registered render resources |
| Camera Kit overlay | Its camera/lens session and output surface | Lens selection and display controls |
| Reading tool | Its Shadow DOM UI and persistent content script | Its declared document/overlay operations |

A package may never take over arbitrary site objects. In particular, a
Three.js package must own the scene it adds, while a site that wants its own
scene controlled must explicitly install its own Cymonkey integration.

## Product composition

The baseline Cymonkey Browser Extension carries no optional library runtime.
A product or deployment composes the desired packages into the extension build.
This lets a target owner choose a small reviewed set—for example, a Three.js
overlay *or* a Camera Kit package—without making every browser installation
carry every SDK.

A browser product is declared in `products/browser/*.json` and built with:

```sh
npm run build:browser-product -- products/browser/camera-kit.json
npm run build:threejs-browser-package
```

The product builder compiles its packages, builds each selected browser,
copies reviewed manifests and artifacts, writes the per-browser registry, and
derives the sandbox CSP from the included package manifests. It refuses
repository-escaping paths and unrecognized CSP sources.

The composed artifact also uses the product manifest's display name in the
browser and popup. This makes two locally loaded reviewed products visibly
distinct—for example, **Cymonkey Three.js Browser Extension** versus
**Cymonkey Camera Kit Browser Extension**.
