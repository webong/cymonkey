# Browser bookmarklets

Jangolova's `jangolova/bookmarklet` library prepares small, user-activated page
augmentations. Cymonkey exposes it through `cmy bookmarklet`. A saved bookmarklet
runs in the current page when clicked; it needs neither an extension nor a
running Cymonkey process.

## Create and install

From the repository root, build the CLI and export the included reversible
link-highlighter:

```sh
go build -o ./bin/cmy ./src
./bin/cmy bookmarklet export \
  --source examples/bookmarklets/highlight-links.js \
  --format html --name 'Highlight links' > /tmp/highlight-links.html
```

Open the HTML file, review the source, and drag **Highlight links** onto your
browser's bookmarks bar. Open an ordinary web page and click the saved bookmark.
Click it again to remove the highlighter. Removing the bookmark itself does
not undo page effects; this example implements its own toggle cleanup.

For manual bookmark creation, export a URL and paste the complete `javascript:`
value into the bookmark's URL/location field:

```sh
./bin/cmy bookmarklet export --source examples/bookmarklets/highlight-links.js
```

The CLI does not edit browser profiles or install bookmarks automatically.
It emits artifacts to stdout; do not redirect output over your input file.

## Import an existing bookmarklet

Save the complete `javascript:` URL in a text file, then decode it for review:

```sh
./bin/cmy bookmarklet import --source saved-bookmarklet.txt > reviewed.js
./bin/cmy bookmarklet export --source reviewed.js --format html --name 'My tool'
```

Import decodes percent escapes exactly once and preserves `+` and literal modulo
operators. It does not execute or fetch code. Exports from this library round
trip to the original source; other bookmarklets retain their own wrappers.

The public Go API is `Encode(source)`, `Decode(url)`, and
`InstallPage(name, source)`. The source limit is 64 KiB; individual browsers
may impose smaller URL or bookmark limits. Encoding accepts a JavaScript function
body, preserves line comments and Unicode, and wraps it with `void` so a string
return value cannot replace the document. Syntax is checked by the browser, not
by the exporter. Use `window` explicitly for persistent globals. Re-exporting
third-party code gives it function scope, which can differ from top-level script
semantics. The installer escapes the source and title and runs no code on load.

## Browser behavior and limits

- Bookmarklets have ordinary page access, not extension privileges or `GM_*` APIs.
- Scripts must implement their own cleanup and repeat-click behavior. Navigation
  discards their page state; automatic reinjection belongs to managed userscripts.
- Content security policy, restricted browser pages, frame boundaries, and
  browser-specific bookmark behavior can prevent execution. No bypass is provided.
- A bookmarklet does not authenticate to the Cymonkey operator or give the page
  access to privileged Jangolova actions. Do not embed operator credentials.
- Safari 18.6 has a manual direct-link activation/toggle check. Native
  bookmarks-bar drag/drop and activation, mobile browsers, and Edge still require
  separate validation. Automated Chrome/Firefox fixtures exercise actual
  `javascript:` links, not browser-toolbar UI or a claimed extension API.

See [MDN's javascript: URL semantics](https://developer.mozilla.org/en-US/docs/Web/URI/Reference/Schemes/javascript)
and [managed userscripts](userscripts.md) for persistent workflows.

## Verification

```sh
go test ./lib/jangolova/bookmarklet ./src
npm run test:browser-adapter
go build -o .cache/cmy-augmentation ./src
CYMONKEY_CMY_BIN="$PWD/.cache/cmy-augmentation" \
CYMONKEY_BROWSER_BIN='/Applications/Chrome.app/Contents/MacOS/Google Chrome' \
npm run test:bookmarklets:live
```

For Firefox use `CYMONKEY_BROWSER_PRODUCT=firefox` and set
`CYMONKEY_BROWSER_BIN` to its executable. Test browsers use disposable profiles.
The fixture verifies import/export, real link execution, repeat-click cleanup,
Unicode, non-navigation on string returns, HTML escaping and normal link CSP
denial. Required executable paths must be supplied; missing prerequisites fail.

For native-toolbar checks, use the exported installer and open
[`tests/fixtures/bookmarklet-manual.html`](../tests/fixtures/bookmarklet-manual.html)
as a separate target page. Its visible status observes the example's style;
it never installs or invokes the bookmarklet. See the
[validation record](augmentation-validation.md) for completed checks and the
remaining Safari toolbar procedure.
