# Live Three.js browser product proof

The Three.js browser product is the first end-to-end proof that Cymonkey can
augment an existing web tab with a reviewed runtime package. It does not make
the Cymonkey Browser Extension a Three.js-specific extension: the extension
discovers, approves, mounts, routes, audits, and unmounts a generic reviewed
package. The `threejs` package owns only the canvas, scene, camera, and objects
it adds.

## Proven flow

```text
reviewed package registry
  -> agent requests augmentation.mount for an existing HTTP(S) tab
  -> Cymonkey popup shows a one-time approval, including target origin
  -> agent retries the unchanged request with approvalId
  -> extension injects the reviewed package and creates its private runtime
  -> cymonkey-engine.call sends typed Three.js actions to that runtime
  -> package mounts its owned overlay, then reports typed results and events
  -> augmentation.unmount removes only the package-owned overlay
```

The live fixture also verifies denial: a denied approval cannot be retried to
mount a second augmentation.

## Run it

From the repository root:

```sh
npm run build:threejs-browser-package
node --test tests/threejs-browser-product-live-test.mjs
```

The test opens an isolated, visible Chrome profile and hosts a tiny local
HTTP page to represent a caller-owned site. It drives the extension's real
popup approval UI, calls the real background/control path, captures
`output/playwright/threejs-browser-product-flow.png`, verifies audit and
lifecycle events, and confirms that the original page is still live after
unmount.

Chrome-branded builds stopped honoring the `--load-extension` command-line
flag in Chrome 137. The test therefore uses Chrome's DevTools
`Extensions.loadUnpacked` command to load the product into its temporary
profile. This is a test-only installation mechanism; normal local use remains
Chrome's **Load unpacked** UI. See Chrome's
[announcement](https://groups.google.com/a/chromium.org/g/chromium-extensions/c/1-g8EFx2BBY)
and its [end-to-end extension testing guide](https://developer.chrome.com/docs/extensions/how-to/test/end-to-end-testing).

Set `CYMONKEY_CHROME_BIN` when Chrome is not at the macOS default executable
path. The test requires a Chrome build with the DevTools extension-loading
command available.

## What is verified

- Chrome live: package discovery, popup approval, approval consumption, mount,
  `threejs.overlay.mount`, `threejs.object.add`, screenshot evidence, audit
  events, unmount, caller-page preservation, and denial.
- Chrome, Edge, Firefox, Safari composition: the product build and static
  package contract verify that each artifact contains the reviewed `threejs`
  manifest and content bundle.

## Remaining browser validation

Edge, Firefox, and Safari do not yet have equivalent loaded-extension browser
automation. Edge shares the Chromium MV3 product shape, while Firefox and
Safari use their respective extension installation and automation paths. A
Safari live test also needs the embedded Safari WebExtension application to be
built and installed. Those are coverage gaps, not capability claims: the
current live evidence is specifically Chrome.
