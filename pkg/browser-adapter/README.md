# Cymonkey browser adapter

`@cymonkey/browser-adapter` supplies the browser-facing pieces that an
integrating extension can compose into its own manager or augmentation host.
It is a library, not an extension build. It does not import WXT and does not
register a service worker, popup, or content script.

```ts
import {createBrowserAdapter, decidePolicy} from '@cymonkey/browser-adapter';

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
asset prefix. Storage keys and network rule IDs are owner-scoped; a different
owner cannot remove or replace another owner's network rule through this
adapter. The package also exports pure `validatePolicy`, `decidePolicy`, and
`validateBrowserPackage` functions. `createControlGate` wraps policy decisions
and redacted audit phases. The integrating extension resolves caller and
target context from trusted browser APIs before passing it to the gate.

`createPackageCatalog` loads a reviewed registry through a caller-supplied
loader. `createApprovalManager` stores expiring, single-use approvals bound to
the package, revision, augmentation, target, origin, and permissions.
`createTabRouter` selects a target and attaches the integrating extension's
own content scripts if a receiver is absent. `createAugmentationManager`
composes these with scoped script injection to mount a reviewed package. The
extension provides its own package assets, content receiver, sandbox page,
approval UI, and manifest permissions.

`createNativeUserscriptManager` manages browser-native registration and
reconciliation. The caller supplies source validation, revision checks,
registration planning, and approval. Pair it with
[`@jangolova/userscript-runtime`](../userscript-runtime/README.md) for those
validation and planning functions. The caller's `describe` function must
return source-free metadata.

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

Pair this with [`@cymonkey/extension-manager`](../extension-manager/README.md)
to list and manage installed extensions, or with the userscript runtime for
userscript-specific validation. Cymonkey/Jangolova's host adapters can manage
installation and browser connections outside an extension.
