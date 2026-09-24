# Cymonkey extension manager library

`@cymonkey/extension-manager` is a small interface for building an extension
manager **inside your own browser extension**. It does not require the Cymonkey
Browser Extension to be installed. Supply your extension's native
`browser.management` or `chrome.management` object:

```ts
import {createExtensionManager} from '@cymonkey/extension-manager';

const manager = createExtensionManager(browser.management);
const installed = await manager.list();
const stopWatching = manager.watch((change) => renderChange(change));

// Call this directly from your UI's click handler. The browser may show its
// own confirmation dialog or reject the action for this particular extension.
disableButton.addEventListener('click', () => {
  void manager.setEnabled(selectedId, false);
});
```

Declare the browser's `management` permission in your extension manifest.
`list()` and `describe(id)` return source-free records. `setEnabled(id, bool)`
and `uninstall(id)` use the browser's native APIs; the browser decides whether
each target can be changed and whether user confirmation is needed. `watch()`
subscribes to installed, enabled, disabled, and uninstalled events and returns
a function to unsubscribe. `capabilities()` reports which adapter methods are
present; individual operations can still be denied by the browser.

## Packaging and installation

The browser management API does not install an arbitrary ZIP or XPI. An
extension can provide a separate trusted host adapter for Cymonkey/Jangolova's
packaging and browser-specific installation flow:

```ts
const manager = createExtensionManager(browser.management, {
  prepare: (source) => trustedHost.prepareExtension(source),
  install: (request) => trustedHost.installExtension(request),
});

const review = await manager.prepare('/caller-owned/extension.zip');
const outcome = await manager.install({
  source: '/caller-owned/extension.zip',
  revision: review.revision,
  target: {id: selectedBrowserProfileId},
});
```

`trustedHost` is an adapter supplied by the integrating application. It can
invoke Cymonkey's `extension.prepare` and `extension.install` actions through
its own authenticated connection. This package never exposes a host file path
or a native installation operation to ordinary web pages. The host must
authorize the request and handle the selected browser's installation flow.

The library is independent of WXT and UI frameworks. The integrating
extension composes these operations into its own manager.
