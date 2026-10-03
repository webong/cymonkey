# Cymonkey browser helper

`@cymonkey/browser-helper` provides APIs for browser integrations, extension
management, userscripts, and page augmentations. An integrating extension or
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

## Extension management

`createExtensionManager` wraps an integrating extension's native
`browser.management` or `chrome.management` API. The extension declares the
`management` permission and calls actions from its own UI. The browser still
decides whether disabling or uninstalling an extension needs confirmation.

```ts
import {createExtensionManager} from '@cymonkey/browser-helper';

const manager = createExtensionManager(browser.management);
const installed = await manager.list();
const stopWatching = manager.watch((change) => renderChange(change));
```

`list`, `describe`, `setEnabled`, `uninstall`, and `watch` operate on installed
extensions through the supplied browser API. `capabilities()` reports which
methods are present. Packaging and installation use an optional trusted host
backend:

```ts
const manager = createExtensionManager(browser.management, {
  prepare: (source) => trustedHost.prepareExtension(source),
  install: (request) => trustedHost.installExtension(request),
});
```

The host handles source review, installation, and native browser consent. The
browser management API does not install arbitrary ZIP or XPI files.

## Userscripts

`createManifest`, `parseMetadata`, `sourceRevision`, `validateManifest`,
`permissionIncrease`, and `registrationPlan` prepare and review a
`cymonkey.userscript/v1alpha1` userscript. `createNativeUserscriptManager`
manages browser-native registration and reconciliation. The caller supplies
source validation, revision checks, registration planning, and approval, and
its `describe` function must return source-free metadata. All of these APIs
are exported from this package's root entry point.

## Cookie export

`createCookieExporter` lets an integrating extension export selected cookies
through its privileged `browser.cookies` or `chrome.cookies` API. It uses the
existing approval manager and control gate, asks for cookie and selected-host
permissions from an approved UI click, checks the selected store, and hands a
bounded payload to a caller-supplied delivery callback. It never falls back to
`document.cookie`. See [browser cookie export](../../docs/browser-cookie-export.md)
for the API, popup integration, scope rules, and consumer handoff boundary.

For a Node.js 22+ host, import `@cymonkey/browser-helper/node`. Its
`discoverLocalCookieSources` lists Chrome, Edge, Firefox, and Safari cookie
sources without reading values; hosts may add custom roots. Then
`createLocalCookieReader` uses a selected source and a caller-supplied
authorization callback. The host can pass an
absolute profile path, a profile name, or `ALL_PROFILES` for Chromium and
Firefox. Safari can take an explicit cookie-file path. The Node entry point
also exports `toCookieHeader` for callers that explicitly need an HTTP header.
The extension-safe root entry point does not import Node APIs.

```ts
import {createLocalCookieReader, discoverLocalCookieSources}
  from '@cymonkey/browser-helper/node';

const sources = discoverLocalCookieSources();
const selected = chooseSource(sources);
const reader = createLocalCookieReader({authorize: (request) =>
  approvedCookieScope(request)});
const result = await reader.querySource(selected, {
  url: 'https://example.com/', names: ['session'],
});
```

The host must select and approve the source and scope before handling returned
values. Local database readers return best-effort portable cookie fields; use
the extension API exporter when native store or partition provenance is needed.

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

Cymonkey and other hosts can manage installation and browser connections
outside an extension. Run `npm run test:browser-helper` at the repository root
to build and test the package.
