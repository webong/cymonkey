# Cymonkey browser integration

Cymonkey is Jangolova's runtime-agnostic augmentation engine. This document
defines its browser integration: the `viewer` platform domain and `render`
document domain over the `browser-dom` runtime,
and CDP, BiDi, Safari MCP, or WebExtension driver. The portable `v1alpha2`
core, macOS integration, ownership model, and migration policy are defined in
[Cymonkey runtime-agnostic augmentation contract](cymonkey-runtime.md).
See [Cymonkey domains, runtimes, and drivers](cymonkey-domains.md) for the
canonical target vocabulary.
See [Cymonkey runtime and driver modules](cymonkey-modules.md) to contribute a
new caller-approved runtime or driver implementation.

The protocol version is `jangolova.cymonkey/v1alpha2`. Its five operations are
also exposed to cooperating page code as:

```js
window.jangolova.cymonkey = {
  hello,
  capabilities,
  describe,
  act,
  events,
};
```

The page global is deliberately less capable than the engine control plane. It
contains page-safe DOM, overlay, description, and event operations only. It
never exposes raw `chrome.*`, raw `browser.*`, arbitrary protocol commands,
extension storage, request interception, or another privileged passthrough.

## Ownership and lifecycle

The target owner or provider owns the browser process, profile, credentials,
extension installation, display, network placement, CDP/BiDi/MCP endpoints,
and endpoint authentication. Jangolova only attaches to those supplied
resources. Disconnecting Cymonkey must not close the browser, remove an
extension, or destroy its profile.

An extension is optional. When extension mode is `auto`, Jangolova attaches to
CDP, BiDi, or Safari MCP first and then probes for an extension backend where
that transport supports a safe probe. A missing extension reduces the
negotiated capability set. It is an error only when extension mode is
`required`. Installation is never attempted by Jangolova.

## Architecture

```text
application or agent
        |
        | jangolova.cymonkey/v1alpha2
        | hello / capabilities / describe / act / events
        v
Cymonkey Control Plane Engine
        |
        +-- driver selection policy: auto | playwright | puppeteer | cdp | bidi | safari-mcp
        +-- capability/origin policy
        +-- capability merger and event cursor
        |
        +-- Playwright driver ---------- Playwright Core CDP automation primitives
        +-- Puppeteer driver ----------- Puppeteer Core CDP / BiDi automation primitives
        +-- Native CDP driver ---------- Runtime / Page / DOM / CSS / Network / Fetch
        +-- BiDi driver ---------------- script / browsingContext / network
        +-- Safari MCP driver ---------- dynamically discovered safe tool mappings
        +-- optional Jangolova Extension - platform services, overlays, and persistent state
                                                    |
                                                    +-- isolated content script
                                                    +-- page bootstrap bridge
```

Hybrid operation merges capabilities from a base automation/interaction transport and the optional
extension. Capability names remain stable; `capabilities` reports which backend driver
will handle each capability and whether another backend driver is available as a
fallback.

## Backend driver selection policy

The default driver is `auto`:

1. Prefer a Playwright or Puppeteer automation driver when direct automation capabilities are required.
2. Fallback to native CDP endpoint as the no-install baseline.
3. Otherwise use a supplied WebDriver BiDi endpoint as a first-class driver backend.
4. Otherwise use a supplied Safari MCP Streamable HTTP endpoint and negotiate
   the safely mapped subset from its discovered tools.
5. If extension mode is `auto` or `required`, probe the optional extension on a
   compatible base transport and merge its persistent/privileged capabilities.
6. Reject the connection when required capabilities cannot be satisfied after
   probing and policy filtering.

An explicit `module` selects one registered contribution; an explicit `driver`
selects a compatible driver implementation. Neither changes the semantic API.
An explicit extension policy is one of:

| Value | Meaning |
| --- | --- |
| `auto` | Use the extension if detected; otherwise continue with reduced capabilities. |
| `disabled` | Do not probe or use an extension. |
| `required` | Fail unless the expected extension handshakes successfully. |

Example:

```json
{
  "driver": "auto",
  "extension": {
    "mode": "auto",
    "id": "optional-provider-supplied-id"
  },
  "policy": {
    "allowedCapabilities": ["document.query", "overlay.mount", "script.execute"],
    "allowedOrigins": ["https://*.wikipedia.org"]
  }
}
```

## Protocol operations

| Operation | Result |
| --- | --- |
| `hello` | Protocol version, implementation, selected backends, and features. |
| `capabilities` | Negotiated, policy-filtered semantic capability descriptors. |
| `describe` | Current backend state, target contexts, augmentations, and extension presence. |
| `act` | Execute one advertised semantic capability. |
| `events` | Non-destructively read events after an opaque cursor. |

Every advertised capability contains:

```json
{
  "name": "script.register",
  "description": "Register a script for matching future documents.",
  "domain": "render",
  "runtime": "browser-dom",
  "driver": "webextension",
  "support": "native",
  "lifetime": "profile",
  "persistence": "persistent",
  "effect": "external",
  "inputSchema": {"type": "object", "required": ["augmentationId", "script"]},
  "alternatives": ["cdp"]
}
```

`support` is `native`, `mapped`, or `emulated`. `lifetime` is `call`,
`document`, `browser-session`, or `profile`. `persistence` is `ephemeral`,
`session`, or `persistent`. These fields describe behavior; they do not grant
authorization.

## Semantic capability set

The `v1alpha2` Cymonkey browser vocabulary separates platform and display
semantics:

- `augmentation.install`, `augmentation.update`, `augmentation.uninstall`
- `augmentation.enable`, `augmentation.disable`, `augmentation.list`,
  `augmentation.describe`
- `render`: `document.query`, `document.observe`, `document.patch`,
  `script.execute`, `script.register`, `script.unregister`, `style.insert`,
  `style.remove`, `overlay.mount`, `overlay.patch`, `overlay.unmount`
- `viewer`: `window.navigate`, `window.click`, `window.fill`,
  `window.press`, `window.evaluate`, `window.screenshot`, `network.observe`,
  `network.rules.install`, `network.rules.remove`, `storage.get`, `storage.set`

Network rules, storage, shared events, and packaged script injection are
implemented and authorized by Jangolova platform services. Script injection
still advertises `render`: its effect is the displayed document. Their
appearance in the Cymonkey vocabulary does not assign ownership to Cymonkey.

A backend advertises only operations it actually supports after runtime
probing. For example, a Safari MCP endpoint with click, type, and screenshot
tools does not thereby advertise augmentation support.

## Backend mapping matrix

| Semantic area | CDP | WebDriver BiDi | Safari MCP | WebExtension |
| --- | --- | --- | --- | --- |
| script execute | `Runtime.evaluate` | `script.evaluate` / `script.callFunction` | mapped evaluate tool | `scripting.executeScript` |
| script register | `Page.addScriptToEvaluateOnNewDocument` | `script.addPreloadScript` | only a discovered preload tool | `scripting.registerContentScripts` |
| script unregister | `Page.removeScriptToEvaluateOnNewDocument` | `script.removePreloadScript` | only a matching removal tool | `scripting.unregisterContentScripts` |
| DOM query/patch | DOM and Runtime domains | `browsingContext.locateNodes` or script | mapped DOM/evaluate tools | isolated content script |
| styles | CSS/Runtime domains | script mapping when supported | mapped style/evaluate tool | `scripting.insertCSS/removeCSS` |
| network observe | Network events | network events | discovered network observation tool | browser events when permitted |
| network rules | Fetch interception, session lifetime | `network.addIntercept/removeIntercept` and actions | only explicit intercept tools | declarativeNetRequest, persistent |
| storage | Jangolova mapping to page/origin storage | Jangolova mapping when script is supported | only explicit discovered tools | Jangolova scoped extension storage |

CDP and BiDi implementations must probe actual browser support. A protocol
method appearing in a specification is not sufficient reason to advertise it.

## Augmentation manifest and persistence

An augmentation is a versioned semantic declaration. The schema is
`src/cymonkey/protocol/v1alpha2/augmentation.schema.json`.

```json
{
  "apiVersion": "jangolova.cymonkey/v1alpha2",
  "kind": "Augmentation",
  "metadata": {
    "id": "wikipedia-reading-tools",
    "revision": "sha256:abc123"
  },
  "spec": {
    "targets": [{
      "domain": "render",
      "runtime": "browser-dom",
      "match": {"urlPatterns": ["https://*.wikipedia.org/wiki/*"]}
    }],
    "permissions": ["document.query", "overlay.mount", "script.register"],
    "render": {"scripts": [{
      "id": "main",
      "source": "globalThis.wikipediaReadingTools = true;",
      "world": "ISOLATED",
      "runAt": "document_start"
    }]}
  }
}
```

The same document can be submitted to CDP and BiDi. Its achieved persistence
depends on the negotiated backend:

- CDP/BiDi registration survives navigation while the attachment remains
  active, but does not promise survival after browser restart.
- Safari MCP persistence is whatever the explicitly mapped tool reports.
- WebExtension registration and extension storage can survive attachment and
  browser restarts, subject to browser/profile policy.

Callers that require a persistence level must request it as a required
capability constraint and verify the returned capability descriptor.

## Trust boundary and control planes

Website content is hostile. Page messages, DOM state, URLs, and page-provided
objects are untrusted. The page global is never an authentication mechanism.

Privileged extension commands use a control plane independent of the page. The
preferred production shape is an extension-initiated authenticated WebSocket
using a caller-supplied endpoint and short-lived token. Backends may instead
use provider-controlled native messaging or CDP evaluation in an extension
service-worker/extension-origin target. The current development backend uses
the latter and verifies the expected extension origin and implementation
handshake. It never dispatches privileged commands through `window.postMessage`.

All private transports feed the same per-capability authorization gate. The
gate evaluates caller, effect, resolved tab/origin, and augmentation ID before
dispatch, then emits redacted requested/succeeded/denied/failed audit events.
The optional outbound WebSocket is implemented in the same artifact and is
activated only by trusted caller configuration. See
[browser-extension control plane](browser-extension-control.md).

The extension consists of:

- a Manifest V3 service worker owning scripting, storage, and declarative
  network request operations;
- an isolated content script implementing bounded page operations;
- a main-world bootstrap that creates `window.jangolova.cymonkey`;
- an extension-origin control entry point for the backend handshake.

The single extension build always carries Xallet Spook integration. It detects
the provider-installed `Xallet` browser hub at runtime, registers when found, and
accepts external privileged calls only from that discovered, enabled hub ID.
When the hub is absent, the same artifact continues to operate standalone.

## Policy requirements

1. Filter capabilities before advertising them and again before dispatch.
2. Apply origin policy to every target context, navigation, augmentation match,
   and network rule.
3. Never expose raw protocol, MCP, or browser-extension API evaluation through
   the public semantic contract.
4. Namespace scripts, styles, rules, storage, and overlays by augmentation ID.
5. Prevent one augmentation from replacing another augmentation's resources.
6. Do not return page contents, source code, credentials, request bodies, or
   extension storage in descriptions/events unless an explicit capability and
   policy allow it.
7. Preserve the caller-owned browser lifecycle on success, error, reconnect,
   and disconnect.
8. Default-deny write/external extension calls until an authenticated bootstrap
   caller installs an explicit fine-grained policy.

## Browser extension package

The optional Jangolova Browser Extension lives in `pkg/browser-ext` and uses WXT. Build it
with:

```sh
npm install --prefix pkg/browser-ext
npm --prefix pkg/browser-ext run check
```

Outputs are `.output/chrome-mv3`, `.output/edge-mv3`,
`.output/firefox-mv3`, and `.output/safari-mv3`. The Safari resources are
embedded in the containing app under `pkg/macos-ext`; its currently unsupported
extension permissions are omitted and the resulting capability set is reduced.
There is no separate normal or Spook build. The target owner loads or installs
the appropriate product; Jangolova does not install it.

## Connection examples

No-install CDP baseline:

```sh
jangolova connect-engine \
  --adapter cymonkey \
  --target-kind browser \
  --endpoint cdp=http://127.0.0.1:9222 \
  --options '{"driver":"auto","extension":{"mode":"auto"}}'
```

First-class BiDi baseline:

```sh
jangolova connect-engine \
  --adapter cymonkey \
  --target-kind browser \
  --endpoint webdriver-bidi=ws://127.0.0.1:9222/session \
  --options '{"driver":"bidi","extension":{"mode":"disabled"}}'
```

Require a provider-installed extension on CDP:

```sh
jangolova connect-engine \
  --adapter cymonkey \
  --target-kind browser \
  --endpoint cdp=http://127.0.0.1:9222 \
  --options '{"extension":{"mode":"required","id":"replace-with-installed-extension-id"}}'
```

Safari MCP uses the same adapter with a caller-owned
`mcp-streamable-http` endpoint. It advertises only the semantic subset derived
from the server's discovered tools.

## Live conformance fixtures

`tests/cymonkey-live-client.mjs` runs one reversible augmentation lifecycle
against either backend. It validates the handshake and capability metadata,
installs/lists/disables/enables/uninstalls the same augmentation document,
queries the DOM, mounts and removes an overlay, checks session storage and
events, and cleans up in a `finally` block. The CDP run additionally installs
and removes a deliberately non-matching owned interception rule.

The target-owning Docker fixtures execute it against Chromium CDP and Firefox
WebDriver BiDi:

```sh
npm run test:cymonkey:live:cdp
npm run test:cymonkey:live:bidi
```

Both fixtures verify that deleting the Cymonkey interaction instance leaves
the independently owned browser process alive.

## Delivery sequence

1. Protocol, trust boundary, ownership, backend matrix, persistence, and policy.
2. Versioned protocol and augmentation schemas.
3. Go backend interface and backend-selection policy.
4. CDP backend and nested page bridge.
5. WebDriver BiDi backend.
6. Safari MCP capability mapper.
7. Optional WebExtension backend and authenticated control plane.
8. Shared conformance and contract tests.
