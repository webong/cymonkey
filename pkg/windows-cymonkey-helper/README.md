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
go build -o cymonkey-windows-helper.exe ./pkg/windows-cymonkey-helper
```

Set these launch variables; do not put them in the JSON configuration:

```text
JANGOLOVA_CYMONKEY_CONTROL_URL=ws://127.0.0.1:...
JANGOLOVA_CYMONKEY_CONTROL_TOKEN=<short-lived-token>
JANGOLOVA_CYMONKEY_PROTOCOL=cymonkey/v1alpha1
JANGOLOVA_CYMONKEY_CONFIG=C:\\absolute\\path\\helper-config.json
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
