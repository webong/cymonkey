# Browser augmentation packages

Cymonkey supplies contracts and libraries for browser augmentations. A
consuming extension or application chooses the packages it reviews, includes
their assets in its own build, declares browser permissions, and owns its UI.

| Library | Role |
| --- | --- |
| [`@cymonkey/browser-adapter`](../pkg/browser-adapter/README.md) | Scoped browser API operations, reviewed catalog, approvals, policy/audit gate, tab and engine routing, mounting, page runtime and bridge, userscript lifecycle and connection hub, sandbox handshake, and optional control/media integrations. |
| [`@cymonkey/extension-manager`](../pkg/extension-manager/README.md) | Installed-extension inventory, enable/disable, uninstall, change events, and an optional trusted host installation adapter. |
| [`@jangolova/userscript-runtime`](../pkg/userscript-runtime/README.md) | Shared userscript validation and registration planning. |
| [`pkg/macos-browser-adapter`](../pkg/macos-browser-adapter/README.md) | Swift catalog and managed-runtime controller for a caller-owned macOS host. |

Jangolova's CDP and WebDriver BiDi worker can attach to a caller-owned
browser endpoint and apply approved augmentations without an extension. The
local [`cmy userscript` flow](userscripts.md) uses this route.

## Integrating extension

An extension can bundle reviewed files below `augmentations/<owner>/`, then
provide its own `storage`, `scripting`, `tabs`, and `declarativeNetRequest`
APIs to the browser adapter. The adapter scopes script IDs, file paths,
storage keys, and network-rule ownership. The catalog validates an explicit
registry and package manifests before mounting. Approvals bind the package,
revision, augmentation, target, origin, and permissions.

The integrating extension supplies authenticated caller identity, browser
manifest permissions, approval UI, content-script entrypoints, any sandbox
iframe, and any offscreen camera implementation. The browser adapter provides
controllers and protocols for these, not an installable extension. Page bridge
responses must stay page-safe, and the consuming host must authorize userscript
port identities before routing actions.

Three.js and Camera Kit remain optional runtime libraries. A consuming
extension decides whether and how to bundle them; Cymonkey does not create a
prebuilt Chrome, Edge, Firefox, or Safari artifact.
