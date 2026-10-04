# Cymonkey userscripts

## Compatibility with userscript managers

Violentmonkey, Tampermonkey, and Greasemonkey all use `.user.js` metadata,
but their grants, sandbox, injection timing, and privileged APIs differ.
Cymonkey treats the declared features as requirements rather than treating a
manager name as a compatibility guarantee. In particular, an omitted `@grant`
is distinct from `@grant none`.

The [Android WebView SDK](../pkg/android-helper/README.md) can inspect any
imported userscript and return a compatibility report before approval. Its
current execution backend supports explicit `@grant none`, bounded HTTPS
`@match`/`@exclude-match` and `@include`/`@exclude` rules. On WebViews with
AndroidX `DOCUMENT_START_SCRIPT` support, it registers scripts in matching
frames and runs them at DOMContentLoaded or shortly afterward. `@noframes`
scripts use the main-frame page-finish path. Scripts needing privileged
`GM_*` or `GM.*` APIs, `@require`, `@resource`, a sandbox, or document-start
timing receive blockers in that report and are not offered as runnable. A
script requiring frames is blocked for a selected WebView without frame
registration support. This applies
equally to scripts authored for all three managers. Future backends can satisfy
more requirements without changing the import decision model.

The shared JavaScript browser helper also requires explicit `@grant none` for
its current registration contract. It does not silently interpret an omitted
grant as page-world execution.

## Local manager without a browser extension

The `cymy userscript` CLI stores approved source in Cymonkey's private local
store, keyed by a browser/profile target ID. It can infer `@name` and `@match`
from a userscript and select a unique discovered local profile when possible.
For example:

```sh
cymy userscript install --source ./my-script.user.js --endpoint cdp=http://127.0.0.1:9222
```

For an existing record, `cymy display connect --userscripts-target ID`
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
supplies. See the [CLI guide](cymy-cli.md#extension-free-userscripts) for the
commands and limits.

## Library for consuming extensions

The shared `cymonkey.userscript/v1alpha1` manifest, validation, and
registration planning live in [`pkg/browser-helper`](../pkg/browser-helper/README.md).
An extension that wants persistent browser-native userscripts can import the
library and supply its own browser APIs, approval UI, storage, and lifecycle.
[`@cymonkey/browser-helper`](../pkg/browser-helper/README.md) can provide
scoped script registration and storage. Cymonkey does not ship an installable
userscript-manager extension.

An integrating extension should present the script name, origins, grants,
source revision, and changes in permissions for review before registration.
It should scope each script to its owner and reject undeclared network,
storage, or native messaging privileges. Page-facing helpers should expose
only the explicitly approved semantic actions.
