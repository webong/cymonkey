# Cymonkey browser helper

`@cymonkey/browser-helper` provides Cymonkey execution APIs for browser
integrations, userscript planning, page augmentations, and the WebRTC device viewer. An integrating extension or
host can use them to build its own manager or augmentation flow. The caller
supplies its own browser entry points, UI, and manifest permissions.

```ts
import {createBrowserAdapter, decidePolicy} from '@cymonkey/browser-helper';

const adapter = createBrowserAdapter({
  storage: browser.storage.local,
  scripting: browser.scripting,
  network: browser.declarativeNetRequest,
  assetPrefix: (owner) => `augmentations/${owner}/`,
});

await adapter.scripts.register('reading-tools', [{
  id: 'page',
  matches: ['https://example.com/*'],
  files: ['augmentations/reading-tools/content.js'],
}]);
const inventory = await adapter.events.read();
```

The caller supplies browser APIs and declares the corresponding manifest
permissions. Individual operations are available only when the supplied API
supports them. Scripts can only reference files under that owner's configured
asset prefix. Storage and network-rule ownership use ext-web's scoped workspace
registries; registered script IDs use ext-web's namespace. The network adapter
must supply both `getDynamicRules` and `updateDynamicRules` so a failed ownership
save can restore the previous browser rules. A different owner cannot remove
or replace another owner's rule through this adapter. There is no read of the
former `cymonkey.scoped.*` or `cymonkey.browserAdapter.networkOwnership.v1`
storage keys and no `cm-*` script-ID fallback. The package also exports pure `validatePolicy`, `decidePolicy`, and
`validateBrowserPackage` functions. `createControlGate` wraps policy decisions
and redacted audit phases. The integrating extension resolves caller and
target context from trusted browser APIs before passing it to the gate.

`createPackageCatalog` loads a reviewed registry through a caller-supplied
loader. `createApprovalManager` stores expiring, single-use approvals bound to
the package, revision, augmentation, target, origin, and permissions.
`createTabRouter` selects a target and delegates bounded tab messaging and
safe HTTP(S) receiver attachment to the pinned `ext-web/native` library.
`createAugmentationManager`
composes these with scoped script injection to mount a reviewed package. The
extension provides its own package assets, content receiver, sandbox page,
approval UI, and manifest permissions.

## Extension management

Generic extension inventory, lifecycle observation, enable/disable, and
uninstall are supplied by `ext-web/native` (`createExtensionCatalog` and
`createExtensionManager`). Cymonkey does not re-export a second management
library. An integrating extension owns its management permission and UI;
Cymonkey still owns any approval and installation policy it applies before
using those native operations. The browser management API does not install
arbitrary ZIP or XPI files.

## Userscripts

`createManifest`, `parseMetadata`, `sourceRevision`, `validateManifest`,
`permissionIncrease`, and `registrationPlan` prepare and review a
`cymonkey.userscript/v1alpha1` userscript. Generic browser-native
registration and reconciliation are supplied by `ext-web/userscripts`
(`createUserscriptRegistry`), not a Cymonkey manager. The integrating host
passes the reviewed Cymonkey registration plan and authorization callback to
that registry; Cymonkey's public descriptions must remain source-free.

## Cookie export

`createCookieExporter` lets an integrating extension export selected cookies
through its privileged `browser.cookies` or `chrome.cookies` API. It uses the
existing approval manager and control gate, asks for cookie and selected-host
permissions from an approved UI click, checks the selected store, and hands a
bounded payload to a caller-supplied delivery callback. It never falls back to
`document.cookie`. See [browser cookie export](../../docs/browser-cookie-export.md)
for the API, popup integration, scope rules, and consumer handoff boundary.

Reading a browser's own cookie stores from a local host belongs to ext's browser
adapters, not to this library. Run `cymy cookies query` (or `normalize`) from
Cymonkey, which asks the selected trusted adapter to read the store and returns
the cookies. The adapter owns profile discovery, store formats and decrypting
values. The extension API exporter above is the right route when native store or
partition provenance is needed.

## Host and page integrations

`OutboundControlClient` is also available for extensions that explicitly
configure an authenticated outbound host connection. Its socket and storage
dependencies are injected, and its status description never returns the token.
`XalletSpookClient` is an optional injected hub bridge.
`createMediaBrokerController` handles tab-scoped camera routing for a host
that supplies its own offscreen document and media implementation.

For content scripts, `createAugmentationRuntimeHost` mounts caller-registered
package factories, while `createDOMPageRuntime` provides bounded DOM summaries
and Shadow DOM overlay actions. `installPageBridgeReceiver` exposes a chosen
set of page-safe actions to `installPageBridgeClient` in the page's main world.
The receiver's `dispatch` must return only page-safe capabilities, descriptions,
and events; the page can call all of those methods directly. HTML and CSS sent
to the overlay runtime must come from a trusted caller.

`createUserscriptConnectionHub` connects browser-native userscript ports to
their declared actions. Its required `authorize` callback must verify that the
script ID and tab belong to the intended installation. `connectSandboxHost`
and `startSandboxGuest` provide the nonce-bound MessagePort handshake for a
caller-owned sandbox iframe. The host supplies a cryptographically random
nonce, creates the iframe, and owns its permission policy and cleanup.
`installEphemeralWebStorage` supplies in-memory storage shims for opaque-origin
sandbox pages.
`createEngineRouter` routes an authenticated caller's request to a selected
tab, package runtime, sandbox, or caller-supplied main-world executor. The
integrating host authenticates that caller before using the router.

## Device viewer

`DeviceViewer` and the minimal browser UI in `viewer/` attach a macOS,
Windows, iOS or Android browser that implements WebRTC to one device session.
This is implementation coverage, not a verified device compatibility matrix.
Build the package (`npm run build:browser-helper`), then serve `viewer/` and
`dist/` from the **same HTTPS origin** as the operator via the host's trusted
reverse proxy (localhost is suitable for development). The UI imports
`../dist/device-viewer.js`. The operator does not serve these static files
automatically.

### Attach

1. The trusted host opens `/v1/device-sessions` with a selected WebRTC adapter,
   `webrtc.signal`, `webrtc.poll` and only the desired media/input grants.
2. With its operator bearer, the host calls
   `POST /v1/device-sessions/{sessionId}/viewer`.
3. Deliver the response's `path` and `token` privately to the viewer. Never give
   the viewer the operator credential or put any credential in a URL.
4. Import `DeviceViewer` from `@cymonkey/browser-helper` (or its
   `@cymonkey/browser-helper/device-viewer` entry point), pass `{url:path, token, video}` and
   call `connect({microphone:false, keyboard:false})`. The included UI allows
   these choices explicitly. Host grants remain authoritative.
5. `close()` sends a best-effort peer bye, stops microphone tracks and closes
   the peer. The host uses DELETE on the session for authoritative revocation.

A viewer credential authorizes only signaling/polling for one session. It cannot
open another session, enumerate devices, access storage, or invoke arbitrary
actions. Issuing again rotates HTTP access; existing media requires session
revocation to stop. Credentials expire with the session (at most one hour).
No cross-origin CORS access is enabled by the bridge.

`iceServers` and `relayOnly` constructor options configure the viewer's ICE path;
the host must also configure its target peer. Supply short-lived TURN credentials
through the trusted host, never in committed examples. `state` events report
connection changes/errors. `restart()` restarts ICE on the same session; an
expired session requires new host authorization and a new viewer instance.

`type(text)` and `press(key)` send atomic actions. The UI exposes text and a named-key picker for Mac/Windows targets; programmatic
`press` also supports the modifier mappings documented in the platform guide. There
are no held keys and no global event listeners that forward typing from other
fields. Pointer click/scroll controls require the explicit pointer option and
separate grants. Drag mode additionally requires `pointer.down`, `pointer.up`,
`pointer.release` and `pointer.move`; it releases on lost pointer capture, blur,
hidden document and disconnect. Microphone upload negotiates Opus
or PCMU according to the target peer. Use Play when mobile autoplay blocks audio.

See [native adapter integration](../../docs/device-platforms.md) for target
adapters, host responsibilities and the unfinished transport work.

For network acceptance, the example UI accepts short-lived ICE server settings and
a relay-only policy. Show connection evidence reports selected route types and
media counters without candidate addresses, credentials or SDP. An empty selected
route does not establish connectivity. See the reference-host acceptance procedure
in `etc/device-hosts/README.md`.

Held pointer sessions own one native button at a time. `down(x,y,button)`,
`move(x,y)`, `up(x,y,button)` and `releasePointer()` support explicit SDK clients.
The viewer refreshes a held position each second; the helper releases and exits
if no input arrives for five seconds. Pointer-enabled sessions must be recreated
after a hold timeout or transport disconnect. Up/release are idempotent when no
button is held. Atomic click/scroll are rejected during a hold.

## Testing

Cymonkey and other hosts can manage installation and browser connections
outside an extension. Run `npm run test:browser-helper` at the repository root
to build and test the package.
