# Cymonkey Android helper

`cymonkey-android-helper` is a Kotlin Android library for an app that owns an
`android.webkit.WebView`. It provides an in-process `browser-dom` binding with
`hello`, `capabilities`, `describe`, `act`, and `events`. The host authenticates
and transports requests, registers its own web views, and owns script review
and any approval UI. The library does not install a browser or extension.

```kotlin
import org.json.JSONObject

val runtime = AndroidWebViewRuntime(authorizeCookieRead = { surfaceId, url, names ->
    approvalState.allowsCookieRead(surfaceId, url, names)
})
runtime.registerSurface("main", WebViewSurface(webView))
runtime.registerApprovedScript(
    "reading-tools", reviewedSource, listOf("https://example.com")
)

// Forward an authenticated Cymonkey request from the host's transport:
val requestJson = JSONObject(payload)
runtime.handle(requestJson) { responseJson -> sendResponse(responseJson) }

// From the host's WebViewClient.onPageFinished:
runtime.onNavigationFinished("main")
```

All calls must run on the UI thread. `userscript.apply` takes a registered
script ID and web-view ID; it cannot carry source. The script is restricted to
an exact HTTPS origin and checks that origin again inside the page. The host
calls `onNavigationFinished` to reapply its registered scripts after
navigation. This hook leaves the host's `WebViewClient` in place.
Replacing a script requires removing its ID and registering the reviewed
replacement explicitly.

`cookie.read` is advertised only when the host supplies `authorizeCookieRead`.
It requires explicit cookie names and returns name/value pairs for the current
page through Android's `CookieManager`. Android exposes this as a request
header, so the helper cannot return cookie attributes such as expiry or
SameSite. The host should handle returned values as sensitive. This does not
read another app's private browser data.

This is an Android library module under `pkg/android-helper`. An embedding app
can include it as a Gradle project dependency and owns its signing, lifecycle,
network transport, and approval flow. The module itself requests no Android
permissions. Device-wide capture or Accessibility integration is a separate
optional adapter requiring the platform's consent flow.
