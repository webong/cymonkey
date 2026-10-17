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
runtime.registerSurface("main", WebViewSurface(webView)) // before webView.loadUrl(...)

// Inspect any imported .user.js file before the app asks the user to approve it.
val report = runtime.userScripts.inspect(reviewedSource, "main")
if (report.runnable) {
    val candidate = runtime.userScripts.prepare("reading-tools", reviewedSource)
    showApproval(candidate.name, candidate.matches + candidate.includes,
        candidate.allFrames, candidate.revision) {
        runtime.userScripts.installApproved(candidate)
    }
} else {
    showUnsupportedFeatures(report.blockers)
}

// Forward an authenticated Cymonkey request from the host's transport:
val requestJson = JSONObject(payload)
runtime.handle(requestJson) { responseJson -> sendResponse(responseJson) }

// From the host's WebViewClient callbacks (main-frame navigation):
runtime.onNavigationStarted("main") // onPageStarted
runtime.onNavigationFinished("main")
```

For persistence, create the runtime with
`userScriptStore = SharedPreferencesUserScriptStore(context)`. The default
store is in memory. The app owns the approval UI, source acquisition, and
update policy. Every update must be prepared and approved again; an update
URL in a script is not fetched automatically. Use `runtime.userScripts.list()`,
`setEnabled(id, enabled)`, and `remove(id)` for management. The host forwards
its existing `WebViewClient` callbacks; this SDK does not replace the client.

The importer reads common Greasemonkey, Tampermonkey, and Violentmonkey
metadata without assuming the script belongs to one manager. `inspect(source)`
returns a compatibility report for any `.user.js` source, including scripts
that this WebView backend cannot yet execute. `prepare` accepts only runnable
scripts. The current backend supports `@name`, bounded HTTPS `@match`,
`@exclude-match`, `@include`, and `@exclude` rules, `@noframes`, page or auto
injection, and `@run-at document-end` or `document-idle`. Scripts without
`@noframes` are registered with AndroidX WebKit's document-start hook, which
runs in each matching frame. A path and exclusion check runs inside every
frame before the script. These scripts execute at `DOMContentLoaded` or on a
later task for idle. A WebView without `DOCUMENT_START_SCRIPT` support is
reported as incompatible for an all-frame script. Register the surface before
its first navigation; newly installed scripts affect future document loads.

Scripts declaring `@noframes` use the main-frame `onPageFinished` hook; idle
is queued on the next UI-loop turn. This runs later than a browser extension's
DOMContentLoaded stage. Removing or disabling a registered frame script takes
effect on the next page load; code already injected into a loading frame may
still execute. `about:blank` and `srcdoc` frames are excluded by the HTTPS URL
check. The SDK does not emit a separate completion event for each frame.
It accepts an explicit `@grant none` only. An omitted `@grant` means a
sandboxed script in modern userscript managers and is reported as unsupported.
No `GM_*` or `GM.*` APIs, privileged network requests, resource imports,
script sandbox, or automatic updates are supplied. The report names missing
capabilities such as `@require`, `@resource`, privileged grants, and
document-start timing; the SDK never silently drops them. Scripts execute in
the page JavaScript world and only on matching HTTPS URLs. They cannot run at
document start through this API. Apps can retain the source and report for a
future backend, but should not offer an unsupported script as installed.

All runtime and manager calls except metadata preparation must run on the UI
thread. `userscript.apply` takes a registered script ID and web-view ID; it
cannot carry source. It applies `@noframes` scripts explicitly; all-frame
scripts use automatic registration. A managed script checks its approved URL
rules again inside the page. Repeated `onPageFinished` callbacks within one
navigation do not reapply a main-frame script. The host should forward
`onPageStarted` for each new navigation. The older
`registerApprovedScript` method remains for caller-supplied exact HTTPS
origins and requires removing its ID before replacing its source.

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
