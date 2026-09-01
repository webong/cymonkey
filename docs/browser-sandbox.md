# Jangolova Browser Extension sandbox

The browser sandbox is an optional delivery environment for a reviewed
augmentation package that needs web-platform features unsuitable for a normal
extension context. It is not a general-purpose remote-code loader and it is
not exposed to the target page.

## Roles

```text
Jangolova control plane
        │ policy-authorized augmentation.mount / cymonkey-engine.call
        │ sensitive permission → popup approval → retry
        ▼
content script ── closed Shadow DOM ── sandboxed extension iframe
        │              MessageChannel              │
        │                                           ├─ reviewed package runtime
        │                                           └─ local WebRTC receiver
        ├─ service worker signaling
        └─ offscreen media broker ── user-approved camera
        ▼
existing target tab
```

The control plane may be Jangolova's standalone popup, an authenticated
outbound WebSocket, or an installed provider integration such as Xallet Spook.
The popup path is first-class and does not wait for either optional integration.

The content script owns the overlay, the iframe, and the message-port endpoint.
The sandbox page owns a package runtime. The page owns neither. A package uses
the normal Cymonkey request shape (`hello`, `capabilities`, `describe`, `act`,
and `events`), so a caller does not need a library-specific extension API.

## Trust boundary

- The sandbox package is bundled below `augmentations/<package>/sandbox.js` at
  product-build time. Jangolova never downloads an npm package or arbitrary
  JavaScript into a running extension.
- A random nonce binds the iframe-ready signal to the specific sandbox instance.
  The iframe receives exactly one `MessagePort`; requests are private to that
  port and scoped to the mounting augmentation.
- Mount completion waits for the packaged runtime to accept its configuration;
  a missing token or failed package connection is returned to the caller instead
  of leaving a half-connected iframe behind.
- Package registration must be a lightweight synchronous bootstrap. Large SDKs
  initialize only after the private channel connects, so an SDK startup failure
  is reported as a connection error rather than being misdiagnosed as a missing
  package.
- The sandbox has no WebExtension APIs. Privileged browser work stays in the
  service worker and content script, where Jangolova policy can authorize and
  audit it.
- Because Chrome requires extension sandboxes to keep a unique opaque origin,
  native `localStorage` and `sessionStorage` are unavailable. The host supplies
  per-iframe in-memory implementations for SDK compatibility. They disappear
  on unmount and never expose the target site's or extension's stored data.
- Chrome requires the sandbox iframe entrypoint to be a web-accessible
  resource before an existing site may embed it. That declaration does not
  grant extension APIs: the manifest sandbox keeps the document in an opaque,
  non-extension origin, and its only Jangolova connection remains the
  nonce-bound `MessageChannel` supplied by the isolated content script.
- A mount must explicitly request its currently supported iframe feature
  permission—for example, `permissions: ["camera"]`. A camera or other
  sensitive browser permission still requires a visible user gesture in the
  package UI. `augmentation.mount` alone does not grant it.
- Sensitive approvals are single-use and bound to the package, augmentation,
  tab, origin, and exact permission set. They expire after five minutes.
- Opaque sandbox documents cannot invoke `getUserMedia`, and the extension is
  not permitted to weaken them with `allow-same-origin`. For Chrome and Edge,
  Jangolova acquires approved camera media in an extension offscreen document
  and relays the track over a local peer connection. Signaling is private
  extension messaging; no signaling server or target-page media access exists.
  Closing the camera or sandbox stops the source tracks and peer connection.

## Why a sandbox exists

Manifest V3 does not permit remotely hosted executable code in ordinary
extension contexts. A sandboxed extension page is isolated from extension APIs
and can have its own CSP. This makes a WebAssembly SDK such as Camera Kit
possible while keeping the browser extension's normal execution surface free
of remote executable code. The package's CSP must be narrowly configured to
the SDK's documented endpoints and reviewed with the package.

## Product composition

The base `pkg/browser-ext` artifact contains the generic sandbox host and an
empty reviewed-package registry. A product manifest selects one or more
approved package bundles. The generic builder copies their canonical manifests,
writes the output registry, and derives the sandbox CSP. The Camera Kit product
build is:

```sh
npm run build:camera-kit-browser-package
```

It creates Chrome and Edge artifacts with
`augmentations/snapchat-camera-kit/sandbox.js`. It does not add Snap to the
base extension dependency graph.

## Browser availability

Chrome and Edge expose the extension sandbox used by this design. Firefox and
Safari still advertise the delivery-neutral `augmentation.mount` and
`augmentation.unmount` actions, but a sandbox-only package is not included in
their product artifact. Their available alternative is a target-owned
`page-runtime` or an ordinary reviewed `augmentation-package` that does not
need sandboxed remote WebAssembly.
