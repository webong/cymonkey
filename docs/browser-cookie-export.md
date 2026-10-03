# Browser cookie export from an integrating extension

`@cymonkey/browser-helper` exports `createCookieExporter` for an extension
that wants to provide a user-approved cookie export. It is a library, not a
first-party Cymonkey extension. The integrating extension supplies its popup,
browser APIs, trusted browser/profile selection, approval UI, and authenticated
delivery channel. Cymonkey owns the approval and audit interaction. A Cymonkey
integration may pass approved results to CTX through CTX's public cookie
contract; CTX does not depend on this exporter or its payload format.

The exporter works in a privileged extension page or background context. A
content script or ordinary web page does not have the required cookies API.
It reads through `browser.cookies` or `chrome.cookies`, including HttpOnly
cookies the extension is permitted to access. It never reads `document.cookie`.
Browser support and permission behavior vary; check `availability()` and
handle permission denial.

The extension declares the `cookies` permission and host access for the sites
it offers. If access is optional, declare it in `optional_permissions` and
`optional_host_permissions` so the approved Export button can call
`permissions.request`. The request must happen directly from that button's
user gesture. For example, a Chromium extension can declare:

```json
{
  "optional_permissions": ["cookies"],
  "optional_host_permissions": ["https://example.com/*"]
}
```

The host pattern is illustrative; the extension must declare every site it
offers before asking for that site's permission. Chrome documents both the
[cookies permission](https://developer.chrome.com/docs/extensions/reference/api/cookies)
and [user-gesture permission requests](https://developer.chrome.com/docs/extensions/reference/api/permissions).
Firefox documents [cookie stores, partitions, and first-party domains](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/cookies).

## Wire the popup to the browser API

The following uses one popup instance for both preparation and the approved
click. `selected` must come from trusted extension or host state, not a page
message. The browser API identifies a cookie store, but does not prove the
host's profile path; the integration must supply and verify that mapping.

```ts
import {
  createApprovalManager, createControlGate, createCookieExporter,
} from '@cymonkey/browser-helper';

const approvals = createApprovalManager({
  storage: browser.storage.local,
  onChange: ({type, approval}) => auditApproval(type, approval),
});
const gate = createControlGate({
  policy: {
    version: 1, defaultDecision: 'deny',
    rules: [{id: 'selected-site', decision: 'allow',
      callers: ['extension-popup'], capabilities: ['cookies.export'],
      effects: ['external'], origins: ['https://example.com']}],
  },
  audit: (event) => auditControl(event),
});

const exporter = createCookieExporter({
  cookies: browser.cookies,
  permissions: browser.permissions,
  approvals, gate, caller: 'extension-popup',
  selected: () => trustedHost.selectedBrowserCookieStore(),
  deliver: (payload) => trustedHost.deliverCookieExport(payload),
});

const request = {
  selection: await trustedHost.selectedBrowserCookieStore(),
  sites: ['https://example.com/account'],
  names: ['session'],
  partition: 'unpartitioned',
};
let prepared = await exporter.prepare(request);
if (prepared.status === 'approval-required') {
  showApproval(prepared.approval, prepared.request);
  await waitForApprovalDecision(prepared.approval.id);
  // After the UI resolves the approval, prepare again with its ID.
  prepared = await exporter.prepare(request, prepared.approval.id);
}
if (prepared.status === 'ready') {
  const grant = prepared.grant;
  exportButton.addEventListener('click', () => {
    // Call immediately from this handler; do not await storage or messaging first.
    void exporter.exportFromUserGesture(grant).then(showResult, showError);
  }, {once: true});
}
```

Use the extension's existing authenticated host channel or an explicit
user-chosen private export destination for `deliver`. Do not send the payload
through a page bridge, content script, console log, analytics event, or an
unauthenticated endpoint. `deliver` receives the cookie values; the method's
ordinary return value contains only a count and selection. Neither the
approval nor control-gate audit callbacks receive values.

`prepare` binds a single-use approval to the exact browser/profile/store,
sites, names, partition, and first-party-domain selection. A second user click
calls `permissions.request` before the first asynchronous wait. The exporter
checks that the selected browser/profile/store is still the same, verifies the
store exists (and belongs to the selected tab when `tabId` is supplied), and
queries each selected site and cookie name with an explicit `storeId`. It
rejects cookies returned for another store, site, name, partition, or
first-party domain. An omitted partition means **unpartitioned only**; provide
`{topLevelSite: 'https://top-level.example'}` to select one partition.
Firefox deployments with first-party isolation should supply the intended
`firstPartyDomain`; the exporter will not silently broaden to all domains.
The integrating extension must explicitly declare support for partition-key
or first-party-domain queries before using those filters; an ignored native
filter could otherwise make a query appear empty or broader than requested.

The delivered `cymonkey.webextension-cookies/v1` JSON preserves native
`storeId`, `firstPartyDomain`, structured `partitionKey` including ancestry,
host-only/domain scope, path, Secure, HttpOnly, session or expiration,
SameSite, and any additional serializable fields returned by the browser.
Exports are bounded to 8 MiB. Permission denial, stale selection, unsupported
APIs, and incomplete native cookie records fail without delivery.

## Local host readers

`@cymonkey/browser-helper/node` now exposes metadata-only
`discoverLocalCookieSources` and `createLocalCookieReader` for a Node.js host.
The caller selects a browser and, for Chromium or Firefox, a profile; an
injected authorization callback receives the exact query before the reader
touches a local cookie store. The reader returns cookie values and warnings
without logging or transmitting them. It uses Sweet Cookie for
cross-platform local reads and offers `toCookieHeader` when a host explicitly
needs an HTTP request header.

On macOS, the separate [`MacOSBrowserCookies`](../pkg/macos-helper/README.md)
Swift product discovers Safari, Chromium, and Firefox stores and reads an
explicitly selected store. Its Keychain interaction is disabled by default.
Both local readers expose fewer browser isolation fields than the privileged
extension API. The host must inspect each reader's scope limits before using
its result as an input to another browser session or CTX.

## CTX handoff

The current CTX generic inline parser accepts portable cookie fields but
rejects populated raw `storeId`, `firstPartyDomain`, and structured
`partitionKey`. A normal browser export contains `storeId`, so passing this
raw payload directly to `ctx share:browser cookie query --inline-stdin` or
`--fallback-stdin` is not yet a valid end-to-end path. Cymonkey's integration
must check the selected browser/profile/store identity and translate this
versioned export into CTX's independent public cookie contract, including its
canonical `partition_key` and namespaced `attributes` fields. CTX owns its
generic record and validation; it does not need to recognize
`cymonkey.webextension-cookies/v1`. Cross-platform JavaScript browser-source
discovery, local cookie readers, and format conversion belong in Cymonkey's
JavaScript packages. A native macOS cookie reader belongs in
[`pkg/macos-helper`](../pkg/macos-helper/README.md). If CTX's public contract
cannot represent a source field without changing its meaning, the integration
must retain the raw scope and report the handoff as unsupported. It must not
drop scope fields to make generic parsing succeed.

This browser export can provide authorized live values when a standalone disk
read is unavailable or stale. It does not make CTX decrypt protected browser
storage or change the browser's permission model.
