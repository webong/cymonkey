# Jangolova Browser Extension

This is the canonical WXT implementation of Jangolova's browser runtime. It
contains shared extension platform services plus three semantic subsystems:

- **Cymonkey** consumes the `web` profile of the runtime-agnostic
  `jangolova.cymonkey/v1alpha2` contract to manage augmentations, DOM
  operations, styles, and overlays.
- **Pacman** transports `jangolova.pacman/v1alpha1` calls to an explicitly installed
  browser presentation runtime such as `@jangolova/threejs-cymonkey`.
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

The privileged extension control plane advertises `v1alpha2` with profile
`web`; the page-safe `window.jangolova.cymonkey` API remains compatible with
the original web-shaped `v1alpha1` projection. Every build
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
