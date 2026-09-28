# Augmentation validation — 2026-09-28

Canonical checkout: `/Users/webong/Workspace/Projects/Akive/cymonkey`.
This pass covers browser augmentation stability and portable bookmarklets.
It does not change the ownership of Board, Blockade, or caller-owned browsers.

## Implemented

- Standalone `jangolova/bookmarklet` library with source/URL round trips and
  escaped, draggable HTML installation pages; exposed as `cmy bookmarklet`.
- Reversible link-highlighter example, preserving comments, Unicode and `%`/`+`.
  Exported scripts cannot replace the page through a string return value.
- Per-augmentation serialization of mount/action/unmount, single cleanup under
  competing unmounts, cleanup retry, and cleanup of invalid factory results.
- Firefox BiDi session termination on detach. Previously the worker exited
  without ending the session, blocking subsequent attachment. The fix preserves
  the browser and current page and removes preload registrations.
- Camera Kit capability lifetime corrected from `document` to the schema's
  `surface`; its descriptor now has a regression check against the shared schema.

## Verified

| Check | Result and scope |
| --- | --- |
| Browser adapter | 23 unit tests pass, including lifecycle races and approval rules. |
| Userscript runtime | 4 unit tests pass. |
| Worker/core contracts | 7 tests pass; generated browser protocol is current. |
| Three.js | 3 runtime tests pass. |
| Camera Kit | 3 contract checks pass; no live camera requested. |
| Go | CLI, userscript store/provider, and all Jangolova packages pass ordinary tests. Bookmarklet package passes `-race`. |
| Chrome + Firefox bookmarklets | Actual links activate, toggle off, preserve the page and Unicode, and obey normal-link CSP denial. |
| Safari 18.6 (20621.3.11.11.3) bookmarklet link | Native Safari opens the exported local installer. Clicking its link adds the gold outline; a second click removes it, preserving the installer URL/title. Native Favourites Bar installation/activation is still pending. |
| Chrome + Firefox consuming web host | Reviewed package approval/denial, mount, DOM mutation, events, unmount, remount, and reload pass. |
| Chrome managed userscripts | Real CLI install/disable and worker navigation/new-tab/unregister/detach tests pass. |
| Firefox managed userscripts | Single BiDi session fixture passes navigation, unregister, detach, reattachment, and preload cleanup. |

The broad Go race build exhausted local disk space. The ordinary affected suite
and the smaller bookmarklet race run passed afterward; a full race-suite pass is
not claimed. Other tasks are editing Board in this shared checkout; those edits
must not be included accidentally when staging augmentation work.

## Reproduce

Build the CLI and browser library first:

```sh
go build -o .cache/cmy-augmentation ./src
npm run test:browser-adapter
go test ./src ./src/internal/userscripts ./src/internal/provider ./lib/jangolova/...
go test -race ./lib/jangolova/bookmarklet
```

For bookmarklet and web-host fixtures, set `CYMONKEY_BROWSER_BIN` to Chrome or
Firefox's executable. Set `CYMONKEY_BROWSER_PRODUCT=firefox` for Firefox:

```sh
CYMONKEY_CMY_BIN="$PWD/.cache/cmy-augmentation" npm run test:bookmarklets:live
npm run test:augmentation:live
```

For Chrome userscripts, set `CYMONKEY_CHROME_BIN` if it differs from the fixture
default, then run:

```sh
CYMONKEY_CMY_BIN="$PWD/.cache/cmy-augmentation" npm run test:userscripts:live
```

Firefox only supports one active BiDi session. Its dedicated fixture launches
a disposable browser without a competing automation client:

```sh
CYMONKEY_FIREFOX_BIN='/Applications/Firefox.app/Contents/MacOS/firefox' \
  npm run test:userscripts:bidi
```

## Remaining platform/product work

- Manual bookmarks-bar installation/activation and Safari, Edge, mobile coverage.
- Privileged consuming-extension fixtures; the web-host fixture uses actual DOM
  but supplies routing and assets instead of invoking WebExtension APIs.
- Broader `GM_*`, `@require`, `@resource`, and automatic userscript updates.
- Signed bundles and publishing; no first-party extension has been reintroduced.
- Live Camera Kit media/consent validation.

## Safari native-toolbar validation handoff

The 2026-09-28 manual pass reached Safari's exported installer and verified
direct link activation and toggle cleanup visually. The Favourites Bar was
hidden; permission to show it temporarily was requested before changing that
view preference. No bookmark has been added and no toolbar setting changed.
The installer and target fixture are open in separate test tabs. Do not count
the direct-link check as native-toolbar validation.

To finish, show the Favourites Bar with approval, drag the installer's
**Highlight links** link onto it, and open
[`tests/fixtures/bookmarklet-manual.html`](../tests/fixtures/bookmarklet-manual.html).
This local page observes the example's style element without invoking it.
Confirm initial “Highlighter absent”, toolbar-click “Highlighter active” with
gold outlines, repeat-click removal, and reload without automatic activation.
Confirm the saved bookmark still works after reload and the target URL/title
do not change. Remove only the temporary test bookmark, close only the test
tabs, and restore the original toolbar visibility. Record the actual result
before marking Safari/native-toolbar work complete.

Bookmarklets do not provide privileged host access. Existing scripts need review
before installation, and cleanup is script-defined. See [bookmarklets](bookmarklets.md).
