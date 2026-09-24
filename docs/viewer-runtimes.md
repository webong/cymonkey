# Viewer runtimes

Cymonkey's `viewer` domain covers two different kinds of desktop attachment.
They share the five-operation contract, but they do not have the same authority
or native implementation.

| Runtime | Purpose | Native drivers |
| --- | --- | --- |
| `macos-app` / `windows-app` | Understand and operate an explicitly allowlisted application. | macOS Apple Events and Accessibility; Windows UI Automation and Win32 window APIs. |
| `macos-viewer` / `windows-viewer` | Observe pixels and inject bounded pointer or keyboard input into an explicitly selected display or window. | macOS ScreenCaptureKit/Core Graphics and Accessibility; Windows Graphics Capture/GDI and `SendInput`. |

Both are **viewer** runtimes. `render` remains the domain for a document,
scene, canvas, camera, or other visual composition that a cooperative runtime
explicitly exposes. A screenshot is an observation of a caller-owned computer display; it
does not turn an arbitrary desktop application into a render runtime.

## Attachment model

```text
caller-owned desktop application or display
             |
             | user consent and target selection
             v
signed native helper (macOS or Windows)
             |
             | authenticated, outbound local WebSocket
             v
Jangolova Cymonkey backend
             |
             v
agent-facing: hello / capabilities / describe / act / events
```

The helper connects to a one-time control endpoint supplied by Jangolova. It
does not accept a listening port, embedded credentials, arbitrary script
source, raw OS API messages, or a request to start/stop a process. Jangolova
does not own the selected application, display server, user session, or
operating-system consent.

## Semantic capabilities

Application runtimes may advertise only the operations they can safely map:

- `window.list`, `window.describe`, and `window.activate` for owner-selected
  top-level windows;
- `ui.query`, `ui.action.invoke`, and `ui.attribute.set` for a bounded,
  attachment-scoped accessibility/UI Automation subtree;
- `app.command.list`, `app.command.describe`, and `app.command.invoke` when
  the owner has registered typed application commands.

Viewer runtimes may advertise only after the selected display/window and native
permission are available:

- `display.describe` and `display.capture`;
- `pointer.move`, `pointer.click`, `pointer.drag`, and `pointer.scroll`;
- `keyboard.type` and `keyboard.press`.

The helper scopes every action to a surface ID returned by `describe`. Pointer
actions are constrained to the surface's logical bounds and any stricter
owner policy. Keyboard text is length-limited and can be redacted in events.
An unavailable screen-recording, accessibility, or Windows secure-desktop
permission removes the affected capability rather than creating a privileged
fallback.

## macOS mapping

`pkg/macos-cymonkey-helper` is the reference application helper. It already
maps allowlisted Apple Event commands and bounded Accessibility calls. Its
viewer extension uses Screen Recording permission for `display.capture` and
Accessibility permission for input injection. Screen capture and input are
kept separate: a user may consent to either one without granting the other.

`pkg/macos-browser-adapter` provides a Swift catalog and managed-runtime
controller for a caller-owned macOS host. The host retains ownership of the
application it augments.

## Windows mapping

`pkg/windows-cymonkey-helper` is the reference Windows helper. It is a
per-user desktop application/helper, not a service: Windows services cannot
reliably or safely interact with the user's interactive desktop. It maps:

- allowlisted top-level-window discovery and activation through Win32;
- bounded UI Automation queries/actions when a future owner-installed UIA
  mapper is enabled; the reference helper deliberately does not claim `ui.*`
  merely because a process has Win32 child windows;
- display capture for an explicitly selected monitor/window; and
- pointer/keyboard injection through Win32 only within the configured viewer
  surface.

It must reject secure desktop surfaces (for example UAC credential prompts),
session-zero targets, protected-content capture failures, cross-integrity
targets that Windows refuses to automate, and arbitrary `SendMessage`,
PowerShell, command-shell, process-launch, or raw UI Automation passthrough.

The Windows helper's outbound control channel follows the same launch material
as macOS:

- `JANGOLOVA_CYMONKEY_CONTROL_URL`
- `JANGOLOVA_CYMONKEY_CONTROL_TOKEN`
- `JANGOLOVA_CYMONKEY_PROTOCOL=cymonkey/v1alpha1`
- `JANGOLOVA_CYMONKEY_CONFIG`

Plaintext `ws://` is accepted only for loopback. A production distributed
helper is signed by its owner; unsigned builds are for local development only.

## Relationship to remote viewers

VNC, WebRTC, RDP, and VM display relays are external *viewer transports*.
They remain caller-owned and are attached through `display-interaction` only
when it has a real transport implementation. The old in-memory reference
transport is a test fixture, not a usable production viewer. Native macOS and
Windows helpers are local viewer runtimes; neither pretends to be VNC or
WebRTC.

## Conformance requirements

Every platform helper must have a fixture that proves:

1. protocol/version and capability metadata are valid;
2. missing consent removes the relevant capability;
3. actions outside an owner-selected surface are denied;
4. stale window/UI element references are rejected;
5. sensitive input is redacted from events; and
6. disconnecting the attachment does not close the target application or
   alter its windows.
