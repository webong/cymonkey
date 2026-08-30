# Browser augmentation packages

`pkg/browser-ext` is Jangolova's browser platform: it owns browser
permissions, target selection, authentication, authorization, audit, storage,
network rules, and packaged-script injection. It is not a catalogue of
JavaScript libraries.

Three.js, Snapchat Camera Kit, or another library are optional augmentation
packages. A package is selected by the product builder or target owner, and is
then shipped as reviewed extension code below:

```text
augmentations/<augmentation-id>/
├── manifest.json
├── content.js
├── sandbox.js
└── assets/...
```

The extension accepts only files below that package directory. It never
downloads and executes an arbitrary npm package or remote JavaScript at
runtime. That preserves WebExtension policy, makes the package reviewable, and
keeps the capability boundary meaningful.

This restriction matters for SDKs that download executable code themselves.
For example, Snap Camera Kit Web downloads its Lens renderer as WebAssembly.
It cannot run in an ordinary MV3 extension or content-script context, but it
can run in Jangolova's explicitly declared extension sandbox. The sandbox is
still shipped and selected as reviewed product code; only the SDK's documented
runtime resources are allowed by that sandbox page's CSP.

## Package contract

A package first has a normal Cymonkey augmentation manifest: it declares the
target, requested permissions, and lifecycle. Its implementation uses
`script.execute` for a one-time action or `script.register` for a persistent
matching content script.

If the package exposes a richer runtime, its content script listens on the
private extension channel `jangolova.cymonkey.augmentation-runtime` and rejects
messages whose `augmentationId` is not its own. It receives only a standard
Cymonkey request (`hello`, `capabilities`, `describe`, `act`, or `events`) and
returns only its declared semantic result.

Jangolova routes that request with:

```json
{
  "type": "JANGOLOVA_EXTENSION_CALL",
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
  "name": "sandbox.mount",
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

Sandboxes are currently available in the composed Chrome and Edge artifacts.
Firefox and Safari builds negotiate the absence of `sandbox.*` rather than
pretend to offer it.

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

The baseline Jangolova Browser Extension carries no optional library runtime.
A product or deployment composes the desired packages into the extension build.
This lets a target owner choose a small reviewed set—for example, a Three.js
overlay *or* a Camera Kit package—without making every browser installation
carry every SDK.
