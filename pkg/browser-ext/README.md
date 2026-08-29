# Jangolova Browser Extension

This is the canonical WXT implementation of Jangolova's browser runtime. It
contains shared extension platform services plus Cymonkey domain integrations:

- **Viewer domain** uses runtime `browser-dom` for window/tab interaction and
  browser-native platform services such as storage and network rules.
- **Render domain** uses runtime `browser-dom` for document operations,
  scripts, styles, and overlays; it can also reach an explicitly installed
  in-page runtime such as `@jangolova/threejs-cymonkey` through the private
  `cymonkey-engine.call` control route.
- **Cymonkey userscripts** use the
  `jangolova.cymonkey.userscript/v1alpha1` payload, require explicit approval,
  and register bounded `@grant none` source through the extension's
  capability-probed native manager.

Jangolova owns extension authentication, policy, packaged script injection,
namespaced storage, declarative network rules, and the shared cursor event log.
Neither subsystem exposes raw `chrome.*` or `browser.*` APIs to the page.

Every privileged call passes the same fine-grained authorization and redacted
audit layer after transport authentication. The single build supports Xallet
Spook, extension-origin/CDP control, and an optional caller-configured outbound
authenticated WebSocket. See `docs/browser-extension-control.md` for policy,
bootstrap, token-expiry, and generated protocol details.

Build the single artifact for each browser with:

```sh
npm install
npm run check
```

The privileged extension control plane advertises `v1alpha2` with domain
`viewer` and runtime `browser-dom`; the page-safe `window.jangolova.cymonkey` API uses the same
`jangolova.cymonkey/v1alpha2` contract. Every build
works standalone and carries the Xallet Spook client. On Chrome, Edge, and
Firefox, when an enabled
`Xallet Hub` is discovered, the extension registers with it and accepts
privileged external calls only from that discovered hub ID. No separate Spook
artifact or installation exists. Safari omits the unsupported discovery and
external-control permissions, so that integration remains unavailable there.

Chrome, Edge, and Firefox builds request their native `userScripts` API and
probe it before enabling source. Safari output omits unsupported permissions,
is embedded by `pkg/macos-ext`, and currently reports userscript execution as
unavailable while still sharing source-free catalog metadata with its
containing app.

The manifest currently permits external messages from extension IDs because
Chrome and Firefox require that declaration before runtime dispatch. Calls are
still rejected unless the sender ID exactly matches the enabled hub discovered
through the management API. Replace the manifest wildcard with published,
stable hub IDs when those IDs are fixed for every supported browser channel.
