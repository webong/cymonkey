# Jangolova Browser Extension sandbox

The browser sandbox is an optional delivery environment for a reviewed
augmentation package that needs web-platform features unsuitable for a normal
extension context. It is not a general-purpose remote-code loader and it is
not exposed to the target page.

## Roles

```text
Jangolova control plane
        │ authorized sandbox.mount / cymonkey-engine.call
        ▼
content script ── closed Shadow DOM ── sandboxed extension iframe
        │              MessageChannel              │
        │                                           └─ reviewed package runtime
        ▼
existing target tab
```

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
- The sandbox has no WebExtension APIs. Privileged browser work stays in the
  service worker and content script, where Jangolova policy can authorize and
  audit it.
- A mount must explicitly request its currently supported iframe feature
  permission—for example, `permissions: ["camera"]`. A camera or other
  sensitive browser permission still requires a visible user gesture in the
  package UI. `sandbox.mount` alone does not grant it.

## Why a sandbox exists

Manifest V3 does not permit remotely hosted executable code in ordinary
extension contexts. A sandboxed extension page is isolated from extension APIs
and can have its own CSP. This makes a WebAssembly SDK such as Camera Kit
possible while keeping the browser extension's normal execution surface free
of remote executable code. The package's CSP must be narrowly configured to
the SDK's documented endpoints and reviewed with the package.

## Product composition

The base `pkg/browser-ext` artifact contains the generic sandbox host only. A
product build may copy one or more approved package bundles into the output
directory and amend the sandbox CSP for those packages. The Camera Kit proof
build is:

```sh
npm run build:camera-kit-browser-package
```

It creates Chrome and Edge artifacts with
`augmentations/snapchat-camera-kit/sandbox.js`. It does not add Snap to the
base extension dependency graph.

## Browser availability

Chrome and Edge expose the extension sandbox used by this design. The current
Firefox and Safari builds do not advertise `sandbox.mount` or
`sandbox.unmount`; a caller should use `capabilities` to negotiate another
delivery mode.
