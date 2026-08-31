# Browser augmentation packages

`pkg/browser-ext` is Jangolova's browser platform: it owns browser
permissions, target selection, authentication, authorization, audit, storage,
network rules, and packaged-script injection. It is not a catalogue of
JavaScript libraries.

Three.js, Snapchat Camera Kit, or another library are optional augmentation
packages. A package is selected by the product builder or target owner, and is
then shipped as reviewed extension code below:

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
keeps the capability boundary meaningful.

Every product artifact also carries `augmentations/registry.json`. A package
is mountable only when it appears in that reviewed registry, its canonical
manifest is present, its delivery supports the current browser, and every
requested iframe permission is declared by the manifest. Merely placing an
unlisted JavaScript file in the artifact does not make it executable through
the Jangolova control plane.

The extension popup is also a standalone package manager. It lists only the
reviewed registry, accepts an ephemeral JSON configuration, targets the active
tab, and runs the package's declared `launch` action after mounting. The
configuration remains in popup memory only; Jangolova does not persist tokens
or copy them into approval or audit records. This path is owned by Jangolova
and works without Xallet or an outbound control connection.

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

An optional package `launch` entry names the first declared capability that a
standalone package manager should invoke after the sandbox has connected. It
does not add a new extension API and cannot name a capability absent from that
package's manifest.

When the mount requests a sensitive permission such as `camera`, the first
call returns `status: "approval-required"` with a short-lived approval ID. The
extension badge indicates a pending request. The user opens Jangolova, reviews
the package, permission, and target origin, and chooses **Allow once** or
**Deny**. An approved caller retries the identical mount with `approvalId`;
the approval is consumed and cannot be reused for another package, origin,
tab, or augmentation. Package configuration, including API tokens, is never
stored in the approval record.

When the user initiates the mount from Jangolova's own popup, approving it
automatically retries that exact in-memory request. Calls from an agent control
plane keep the same explicit retry behavior. Xallet Spook and authenticated
WebSocket callers are optional integrations; neither is required for the
standalone popup path.

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

A browser product is declared in `products/browser/*.json` and built with:

```sh
npm run build:browser-product -- products/browser/camera-kit.json
```

The product builder compiles its packages, builds each selected browser,
copies reviewed manifests and artifacts, writes the per-browser registry, and
derives the sandbox CSP from the included package manifests. It refuses
repository-escaping paths and unrecognized CSP sources.
