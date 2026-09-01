# Cymonkey userscripts

Userscripts are user-installed JavaScript programs that run on matching web
documents. Cymonkey owns their semantic lifecycle as a high-trust augmentation
form. They are not a separate Cymonkey subsystem, ordinary packaged scripts,
or a raw extension API.

The manifest payload is `cymonkey.userscript/v1alpha1`. Its shared
validation and registration planning live in `pkg/userscript-runtime` and are
consumed by the Cymonkey extension manager in WXT and the Safari WebExtension
embedded in `pkg/macos-ext`. Cymonkey augmentation manifests may carry these
payloads under `spec.web.userscripts`.

## User surface

The initial Cymonkey capabilities are:

- `userscript.prepare`
- `userscript.install`
- `userscript.update`
- `userscript.uninstall`
- `userscript.enable`
- `userscript.disable`
- `userscript.list`
- `userscript.describe`

Callers discover these through Cymonkey `capabilities` and invoke them through
`act`, for example:

```json
{
  "method": "act",
  "params": {
    "name": "userscript.prepare",
    "input": {"id": "example-enhancer", "name": "Example Enhancer", "matches": ["https://example.com/*"], "code": "document.documentElement.dataset.enhanced = 'true';"}
  }
}
```

Cymonkey `describe` returns manager availability and source-free installed
script descriptions. Lifecycle notifications use Cymonkey `events`.

Installation accepts a versioned manifest plus source. It never accepts browser
API objects or a request to bypass extension policy. A script has a stable ID,
revision, display name, match/exclude patterns, run timing, execution world,
declared grants, source provenance, and enabled state.

The MVP accepts `@grant none` only. Brokered Greasemonkey/Tampermonkey-style
grants require separate, schema-described Cymonkey capabilities in a later
version. Undeclared network, storage, native messaging, and extension API
access are not inferred from source text.

## Installation and consent

Arbitrary userscript source is privileged. `userscript.prepare` turns a bounded
agent-authored script body into the standard manifest and metadata header; it
does not store, register, or execute anything. It rejects a supplied metadata
block so the prepared match patterns, execution world, and `@grant none`
declaration are the values the user sees.

The caller then sends that exact manifest to `userscript.install` (or
`userscript.update`). Cymonkey returns an approval request rather than trusting
an `approved: true` field from the caller. The extension popup displays a
source-free summary, and the user may approve it once. The caller retries with
the returned approval ID and the unchanged manifest; the approval is consumed
only when its operation, script ID, revision, and public permissions match.
Updates always require a new approval because their source revision changes.

In other words, the control-plane exchange is deliberately two-phase:

```text
userscript.prepare(draft)              -> manifest (no execution)
userscript.install({manifest})         -> approval-required + approval.id
user approves that item in popup       -> approval becomes usable once
userscript.install({manifest, approvalId}) -> registered userscript
```

The final request must reuse the manifest byte-for-byte. Changing its source,
matches, world, or other reviewed metadata produces a different revision or
approval scope and requires another review.

## Managed script runtime

Scripts prepared by `userscript.prepare` receive a small, page-local
`globalThis.cymonkey.jangolova` helper. It is not a browser-extension API and
does not expose storage, network, tabs, or privileged extension methods. Its
purpose is to let the script explicitly describe the semantic operations that
an agent may request and the events it may emit:

```js
globalThis.cymonkey.jangolova.register({
  'notice.set': ({text}) => {
    document.documentElement.dataset.agentNotice = String(text);
    return {shown: true};
  },
});

globalThis.cymonkey.jangolova.emit('notice.ready', {location: location.href});
```

The authenticated control plane discovers a connected script with
`userscript.runtime.describe` and calls its declared operation through
`userscript.call`. Each call is routed to the script in the specified tab and
returns only the script's own result. This is how an agent controls a custom
augmentation after installation; it never gets raw extension privileges.

Managed control is available only for `USER_SCRIPT` scripts on browsers that
support the native `userScripts` messaging world. A `MAIN` world script remains
an approved, page-visible script, but cannot receive this private Cymonkey
control channel because pages may observe or interfere with that world.

This gives an agent two deliberate augmentation paths:

1. For lightweight, page-local behavior, prepare a userscript, get one-time
   user approval, and install it through the browser-native `userScripts` API.
2. For a stateful runtime or bundled dependency such as Three.js, create a
   reviewed augmentation package, include it in the Cymonkey product build,
   and mount that package under its declared permissions.

Neither path permits a page or an external caller to bypass the extension's
approval and control policy. The page-safe projection of
`window.cymonkey.jangolova` does not advertise userscript capabilities.

Before enabling a script, the UI must show:

- name, namespace, version, description, and source origin;
- requested match and exclude patterns;
- execution world and run timing;
- declared grants;
- whether an update URL is configured.

Remote update URLs must use HTTPS. An update is staged and revalidated; it does
not silently widen matches, grants, or execution world. A material permission
increase requires renewed approval.

## Runtime adapters

The shared package parses, validates, normalizes, compares permissions, and
produces a browser-neutral registration plan. It does not call WebExtension
APIs itself.

The Cymonkey extension supplies the browser manager/backend. Its preferred
runtime adapter is the MV3
`userScripts` API, which is designed for user-provided arbitrary code. The
extension advertises userscript execution only after probing the API with a
real method call. Browser-level user settings may make the API unavailable
even when the manifest permission exists.

There is deliberately no `eval` or `Function` fallback in an extension
service worker/content script. A browser without a verified arbitrary-code
userscript execution environment may still import a disabled script, inspect
it, store it, or remove it, but reports runtime state `unavailable`; enabling
or installing an enabled script fails safely and no source is executed.

Chrome requires the `userScripts` permission and a user-controlled setting.
Firefox MV3 also provides a dedicated `userScripts` API. Safari support is
capability-probed in the embedded WebExtension; Cymonkey does not infer it
from generic `scripting` support.

## Storage and lifecycle

The canonical extension-side record is stored under a Cymonkey-owned
namespace in `browser.storage.local`. Source is size-bounded and never copied
into events or descriptions. Descriptions return source metadata and hashes,
not source code, unless an authenticated caller explicitly requests a source
export capability that is not part of the MVP.

On extension startup/update, the manager reconciles enabled stored records with
the browser's registered userscripts. It removes orphan registrations and
restores missing approved registrations. Browser registration IDs are derived
from stable script IDs and never supplied directly by page code.

Events include install/update/enable/disable/uninstall and runtime availability
changes. Event payloads omit source and credentials.

## Ownership boundary

Cymonkey owns the programmatic contract: install, update, uninstall,
enable/disable, list, describe, and lifecycle events. The Cymonkey extension
manager implements browser-native approval, persistence, registration,
reconciliation, and the Safari native metadata bridge. Cymonkey never exposes
that manager's raw `chrome.*`, `browser.*`, or Safari APIs.
