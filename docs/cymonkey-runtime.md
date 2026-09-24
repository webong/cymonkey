# Cymonkey runtime entry and augmentation contract

Cymonkey is Jangolova's runtime-entry and augmentation layer. It gains
approved access to caller-owned targets, mounts reviewed runtime packages, and
coordinates their use. Jangolova owns the runtime libraries that execute
interaction and presentation semantics inside Three.js, Godot, Unity, Unreal, Blender,
browser, and future runtimes. Direct browser automation tools (Playwright,
Puppeteer, raw CDP, WebDriver BiDi) are entry drivers used by Cymonkey to reach
Jangolova capabilities; they are not separate top-level products.

The runtime-agnostic protocol is `jangolova.cymonkey/v1alpha2`. Its canonical
interaction domains are [`viewer`, `render`, and `player`](cymonkey-domains.md).
`v1alpha2` is the sole Cymonkey wire contract, including the browser page
bridge.

The canonical schemas and portable conformance suite live with the standalone
core in `protocol/cymonkey/` and `tests/cymonkey-core-conformance.mjs`. Jangolova
integrations consume those assets; they do not define a separate protocol.

## Boundary and ownership

Cymonkey owns:

- runtime discovery and approved entry (extension installation, target
  selection, mounting, and private routing);
- augmentation manifests, reviewed package registry, and lifecycle;
- component hosting and cross-subsystem coordination;
- entry-driver configuration and resource ownership for the access path.

Jangolova owns:

- runtime libraries and adapters for interaction and presentation;
- semantic surface/resource registration and runtime-native action handling;
- target attachment, authentication, authorization, consent, and policy;
- transport, event buffering, storage, networking, script execution, reconnect,
  and credential-renewal behavior.

The target owner or provider owns the application/runtime process, profile,
documents, windows, display, GPU, credentials, installation, and lifecycle.
Disconnecting Cymonkey detaches Jangolova; it never quits the target, closes its
documents, or revokes user-granted operating-system permissions.

Jangolova runtime libraries expose explicit scene, camera, object, material,
animation, timeline, UI, and artifact resources through the shared semantic
protocol (`hello`, `capabilities`, `describe`, `act`, `events`). Cymonkey only
routes an approved request to the selected runtime library.

## Protocol shape

Every domain implements the same five operations:

| Method | Meaning |
| --- | --- |
| `hello` | Negotiate the exact protocol, domains, runtimes, implementation, and active drivers. |
| `capabilities` | Return policy-filtered, schema-described operations actually supported now. |
| `describe` | Describe target surfaces and installed augmentations without leaking unrestricted target data. |
| `act` | Invoke one advertised semantic capability. |
| `events` | Non-destructively read bounded semantic events after an opaque cursor. |

A capability identifies its domain, runtime, and driver:

```json
{
  "name": "ui.action.invoke",
  "domain": "viewer",
  "runtime": "macos-app",
  "driver": "macos-accessibility",
  "support": "mapped",
  "lifetime": "attachment",
  "persistence": "session",
  "effect": "write",
  "inputSchema": {
    "type": "object",
    "required": ["surfaceId", "elementId", "action"]
  }
}
```

`support` is `native`, `mapped`, or `emulated`. `lifetime` is `call`,
`surface`, `attachment`, or `installation`. `persistence` is `ephemeral`,
`session`, or `persistent`. These fields describe achieved behavior; they do
not grant authority.

## Core vocabulary

The portable core is deliberately small:

- `augmentation.install`, `augmentation.update`, `augmentation.uninstall`
- `augmentation.enable`, `augmentation.disable`, `augmentation.list`
- `augmentation.describe`
- `surface.list`, `surface.describe`
- `overlay.mount`, `overlay.patch`, `overlay.unmount` in the `render` domain
  when the selected host provides an owned overlay surface

Domain-specific operations are never inferred from the existence of a generic
automation tool. Every capability must be probed, policy-filtered, and
advertised with its input schema before `act` accepts it.

## Target-neutral augmentation manifest

`v1alpha2` replaces browser-only `matches` with typed targets:

```json
{
  "apiVersion": "jangolova.cymonkey/v1alpha2",
  "kind": "Augmentation",
  "metadata": {
    "id": "music-companion",
    "revision": "sha256:abc123"
  },
  "spec": {
    "targets": [{
      "domain": "viewer",
      "runtime": "macos-app",
      "match": {"bundleId": "com.apple.Music"}
    }],
    "permissions": [
      "app.command.invoke",
      "ui.query",
      "ui.observe",
      "overlay.mount"
    ]
  }
}
```

The manifest requests semantic permissions. It does not contain credentials,
TCC grants, entitlements, resolved endpoints, raw AppleScript, or arbitrary
browser-extension API calls.

## Browser runtime: viewer platform and render document

The `browser-dom` runtime is consumed by the Jangolova Browser Extension and by
Jangolova's CDP, WebDriver BiDi, and Safari MCP backends. It exposes
`viewer` for browser/platform controls and `render` for the displayed
document and the assets that augment it.

Its render vocabulary includes:

- `document.query`, `document.observe`, `document.patch`
- `script.execute`, `script.register`, `script.unregister`
- `style.insert`, `style.remove`
- `overlay.mount`, `overlay.patch`, `overlay.unmount`

Its viewer vocabulary includes `window.*`, browser/extension storage, and
network observation or rules. A script or stylesheet affects the displayed
document, so it is always described as `render`, even when an extension or a
browser automation driver performs the underlying call.

Web targets match origins and document URLs. The public
`window.jangolova.cymonkey` bridge remains a page-safe projection of the web
domain. Privileged operations stay on Jangolova's authenticated control plane.
The browser extension consumes the Cymonkey contract; it is not the contract.

## Viewer domain: macOS runtime

The `viewer` domain with runtime `macos-app` has two complementary driver families.

### Apple Events

Apple Events are structured interprocess messages understood by applications
that expose scripting terminology. AppleScript is one language that produces
Apple Events; it is not the semantic API Cymonkey should expose.

The macOS runtime maps discovered and allowlisted scripting commands to:

- `app.command.list`
- `app.command.describe`
- `app.command.invoke`

Commands are scoped by target bundle identifier and, when available, scripting
access group. Cymonkey never advertises `applescript.execute` or a raw Apple
Event passthrough. The backend translates typed inputs into an allowlisted
event only after authorization.

Apple documents Apple Events as structured messages used to request operations
and receive replies across process boundaries:
<https://developer.apple.com/documentation/coreservices/apple_events>.

### Accessibility

Applications without useful scripting terminology may expose an Accessibility
hierarchy. Jangolova maps bounded `AXUIElement` operations to:

- `ui.query`
- `ui.observe`
- `ui.action.invoke`
- `ui.attribute.set`

Accessibility identifiers are observations, not automatically stable semantic
IDs. A backend must scope them to an attachment and reject stale references.
It may expose only supported actions and settable attributes. It must not dump
the entire system-wide accessibility tree by default.

Apple's Accessibility API represents accessible application elements and
supports bounded attribute access, actions, mutation, and notifications:
<https://developer.apple.com/documentation/applicationservices/axuielement_h>.

### Consent and sandbox policy

macOS authorization is part of capability negotiation:

- Apple Events require Automation consent and, for sandboxed applications,
  target-specific scripting entitlements or permitted exceptions.
- Accessibility operations require the user's Accessibility authorization.
- Missing consent produces reduced or unavailable capabilities; Cymonkey never
  bypasses the operating system prompt or silently falls back to unrestricted
  script execution.

Apple documents target-specific scripting entitlements and sandbox limits at
<https://developer.apple.com/library/archive/documentation/Miscellaneous/Reference/EntitlementKeyReference/Chapters/EnablingAppSandbox.html>.

## Driver negotiation

Jangolova selects drivers compatible with the caller-owned target and merges
only compatible capabilities:

| Domain and runtime | Drivers |
| --- | --- |
| `viewer` + `render` / `browser-dom` | CDP, WebDriver BiDi, Safari MCP, Jangolova Browser Extension |
| `viewer` / `macos-app` | Apple Events, Accessibility, caller-owned cooperative native helper |
| `render` / `threejs`, `godot`, `unity`, `unreal` | in-page runtime or authenticated Cymonkey WebSocket |
| `player` / runtime-specific | only a runtime that advertises typed player capabilities |

Hybrid viewer-domain operation may combine Apple Events for application commands with
Accessibility for UI observation. The merged description retains backend
provenance for every capability. A command name discovered through Apple Events
does not authorize an Accessibility mutation, and vice versa.

## Native helper attachment

`pkg/macos-cymonkey-helper` is the reference Swift binding. It uses AppKit's
Apple Event descriptors and the `AXUIElement` APIs, but remains a process owned
by the native target provider:

1. Jangolova creates an ephemeral loopback WebSocket control host and a random
   bearer token for a `macos-application` interaction instance.
2. The authenticated initial connection response returns one-time caller-launch
   environment containing the control URL, token, and exact Cymonkey protocol.
3. The target owner adds its helper configuration path, then launches its own
   signed helper. Jangolova never invokes the executable.
4. The helper connects outward, and Jangolova validates its `v1alpha2`
   `viewer`/`macos-app` declaration plus every capability descriptor before
   accepting actions.
5. Disconnect closes the control host. It does not quit an application, revoke
   TCC consent, or manage the helper's signing identity.

The owner configuration maps semantic command names to fixed four-character
Apple Event class/ID pairs and parameter schemas. The helper checks that the
target application is already running, preventing command dispatch from being
used as an application launcher. Accessibility searches enforce depth/result
limits, and returned element IDs are valid only for the current process-backed
surface attachment.

Missing Accessibility consent removes `ui.*` from the negotiated capability
set. Automation consent remains target/command-specific and is enforced by
macOS when an allowlisted command is invoked. A target owner signs or embeds
the helper using its own identity and entitlements; repository builds do not
attempt ad-hoc or production signing.

## Standard contract

All callers use `v1alpha2` and inspect `domain`, `runtime`, and `driver`.
Browser manifests use typed `render` / `browser-dom` targets for document
augmentations and `viewer` / `browser-dom` targets for platform work; capability
lifetimes are `call`, `surface`, `attachment`, or `installation`.

## Initial delivery status

1. Publish this contract and the `v1alpha2` schemas.
2. Add shared Go protocol/domain validation independent of target kind.
3. Adapt the current browser implementation as the `viewer` and `render` /
   `browser-dom` runtime.
4. Add a macOS capability mapper boundary for Apple Events and Accessibility.
5. Add fake backends and shared conformance tests before binding native APIs.
6. Implemented a caller-owned Swift macOS helper and authenticated reverse
   attachment without adding target lifecycle ownership to Jangolova.
