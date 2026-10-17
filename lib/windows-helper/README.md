# Cymonkey Windows Helper

This is Jangolova's reference native helper for the Cymonkey `viewer`
domain on an interactive Windows desktop. It connects outward to Jangolova's
one-time authenticated WebSocket endpoint and implements
`cymonkey/v1alpha1`.

It deliberately exposes only owner-configured executable names. The current
reference mapping provides top-level window discovery/activation and, when the
optional viewer policy is enabled, capture and bounded pointer/keyboard input
for those windows. It does **not** provide arbitrary Win32 messages, shell or
PowerShell execution, process launching, arbitrary UI Automation inspection,
or control of the Windows secure desktop.

## Build

Build on a Windows machine or cross-compile from the repository root:

```powershell
go build -o cymonkey-windows-helper.exe ./src/windowshelper
```

Set these launch variables; do not put them in the JSON configuration:

```text
CYMONKEY_CONTROL_URL=ws://127.0.0.1:...
CYMONKEY_CONTROL_TOKEN=<short-lived-token>
CYMONKEY_PROTOCOL=cymonkey/v1alpha1
CYMONKEY_CONFIG=C:\\absolute\\path\\helper-config.json
```

Plaintext WebSocket is accepted only on loopback. The helper should run in the
interactive user session and be signed by the distributor for production use.
It must not run as a Windows service.

## Viewer policy

Viewer support is disabled unless `viewer.enabled` is true. `allowCapture` and
`allowInput` are separate because observation and input have different risk.
Every viewer action requires a surface returned by `describe`; the helper
checks its current bounds immediately before dispatch. `blockedKeys` is an
extra owner policy boundary, and sensitive text is redacted in events.

Windows may reject activation/input for a different integrity level, session,
or the secure desktop. Those failures are returned as semantic unavailable or
denied results; they never trigger an elevated fallback.

## App-owned WebView2 scripts and extensions

The separate [WebView2 class library](WebView2/Cymonkey.Windows.WebView2.csproj)
binds to a host application's `CoreWebView2` and its selected profile. It can
register approved scripts for future documents, run a registered script in
the current page, and remove its future-document registration. Scripts check
exact HTTPS origins inside the page. The library also lists extensions in the
selected profile and, after a host approval callback, installs an unpacked
extension directory or enables, disables, or removes an installed extension.

The host must enable extensions when it creates its WebView2 environment:

```csharp
var options = new CoreWebView2EnvironmentOptions {
    AreBrowserExtensionsEnabled = true
};
var environment = await CoreWebView2Environment.CreateAsync(
    null, userDataFolder, options);
await webView.EnsureCoreWebView2Async(environment);

var helper = new WebView2BrowserHelper(
    webView.CoreWebView2,
    request => approvalUI.AuthorizeAsync(request));
await helper.InstallApprovedDocumentScriptAsync(
    "reading-tools", reviewedSource, new[] { "https://example.com" });
var installed = await helper.InstallApprovedExtensionAsync(unpackedFolder);
```

Add `using Cymonkey.Windows.WebView2;` and
`using Microsoft.Web.WebView2.Core;` in the host. The host chooses the
WebView2 environment and profile, reviews scripts and extension files,
supplies approval UI, and calls these APIs on the WebView2 UI thread. The
extension directory must remain available because WebView2 does not copy its
contents into the profile. Extension installation persists in that WebView2
profile; it does not install into Microsoft Edge or another browser profile.
Build the library on Windows with `dotnet build
lib/windows-helper/WebView2/Cymonkey.Windows.WebView2.csproj`.

The existing Go executable remains a desktop-window helper. It does not
attach to another app's WebView2 or expose these APIs through its WebSocket
control connection. An embedding app can route its own authenticated Cymonkey
requests to this library.
