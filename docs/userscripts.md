# Cymonkey userscripts

## Local manager without a browser extension

The `cmy userscript` CLI stores approved source in Cymonkey's private local
store, keyed by a browser/profile target ID. It can infer `@name` and `@match`
from a userscript and select a unique discovered local profile when possible.
For example:

```sh
cmy userscript install --source ./my-script.user.js --endpoint cdp=http://127.0.0.1:9222
```

For an existing record, `cmy provider connect-engine --userscripts-target ID`
attaches to the supplied browser endpoint. Jangolova registers each enabled
script through CDP or WebDriver BiDi and reapplies it after navigation.
Cymonkey watches the store during that connection and reconciles installs,
updates, enablement, and removals. A later connection loads the same records.
If the current document loaded before attachment, the script runs when
attachment completes.

This backend runs in the page world at document start. It supports
`@grant none` and bounded URL patterns. It does not provide privileged
userscript grants or browser-native execution while Cymonkey is offline. The
caller must associate the target ID with the actual browser endpoint it
supplies. See the [CLI guide](cmy-cli.md#extension-free-userscripts) for the
commands and limits.

## Library for consuming extensions

The shared `cymonkey.userscript/v1alpha1` manifest, validation, and
registration planning live in [`pkg/userscript-runtime`](../pkg/userscript-runtime/README.md).
An extension that wants persistent browser-native userscripts can import the
library and supply its own browser APIs, approval UI, storage, and lifecycle.
[`@cymonkey/browser-adapter`](../pkg/browser-adapter/README.md) can provide
scoped script registration and storage. Cymonkey does not ship an installable
userscript-manager extension.

An integrating extension should present the script name, origins, grants,
source revision, and changes in permissions for review before registration.
It should scope each script to its owner and reject undeclared network,
storage, or native messaging privileges. Page-facing helpers should expose
only the explicitly approved semantic actions.
