# Cymonkey iOS helper

`CymonkeyIOSHelper` is a Swift package for a caller-owned iPhone or iPad app
that contains a `WKWebView`. It provides an in-process `browser-dom` binding
with `hello`, `capabilities`, `describe`, `act`, and `events`. The host registers
its own web views, authenticates requests, and chooses how to transport them.
The package does not install an app or a Safari extension.

```swift
import CymonkeyIOSHelper

let runtime = IOSWebViewRuntime(authorizeCookieRead: { surfaceID, url, names in
    await approvalUI.allowCookies(on: surfaceID, for: url, names: names)
})
try runtime.register(surface: WKBrowserSurface(webView), id: "main")
try runtime.registerApprovedScript(
    id: "reading-tools", source: reviewedSource,
    allowedOrigins: [URL(string: "https://example.com")!]
)

// Forward an authenticated Cymonkey request from your app's transport:
let response = await runtime.handle(request)

// From your WKNavigationDelegate after navigation completes:
await runtime.navigationDidFinish(surfaceID: "main")
```

The host owns script review and supplies source only through
`registerApprovedScript`; a remote `userscript.apply` action takes a registered
ID and surface ID, never source. Scripts are allowed on exact HTTPS origins
and check the page origin again inside JavaScript. `navigationDidFinish`
reapplies registered scripts while the host is running. The helper does not
replace the host's navigation delegate or install a persistent userscript
outside its web view.
Replacing a script requires removing its ID and registering the reviewed
replacement explicitly.

`cookie.read` appears only when the host supplies `authorizeCookieRead`. It
requires explicit names, runs the host's approval callback, rechecks the page
after approval, and returns name/value pairs for cookies matching that page.
The host should handle these values as sensitive and decide whether to deliver
them. This uses the registered web view's `WKHTTPCookieStore`; it does not read
Safari or another app's cookie store.

The helper has no network server or background service. A host can embed it
in an app and route authenticated requests to `handle`. Safari integration,
if desired by another product, requires that product's own iOS app containing
a Safari Web Extension and the user's Safari enablement and site permissions.

Run `swift test --package-path lib/ios-helper` on macOS for the protocol and
policy tests. The package also declares an iOS deployment target; device and
simulator validation belong to the embedding app.
