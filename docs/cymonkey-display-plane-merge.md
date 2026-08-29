# Cymonkey display-plane consolidation

Cymonkey is the single semantic contract for Jangolova augmentation across
computer applications, renderers, and player sessions. Its `render` domain
provides explicit engine-resource control.

## Target vocabulary

Every v1alpha2 attachment and capability states three independent facts:

| Field | Meaning | Examples |
| --- | --- | --- |
| `domain` | Interaction plane | `computer`, `render`, `player` |
| `runtime` | Concrete implementation inside that plane | `browser-dom`, `macos-app`, `threejs`, `godot`, `unity`, `unreal` |
| `driver` | Mechanism used to connect or execute | `cdp`, `bidi`, `webextension`, `accessibility`, `apple-events`, `cymonkey-ws`, `in-page-runtime` |

This avoids treating a browser as an application runtime or treating Three.js
as a browser. A browser page containing Three.js has two attachable domains:
`computer/browser-dom` for page and browser work, and `render/threejs` for
explicitly registered scene resources. A goal may coordinate both attachments.

## Consolidated contract

`jangolova.cymonkey/v1alpha2` supplies the same five operations everywhere:

```
hello → capabilities → describe → act → events
```

The render domain guarantees:

- Only resources deliberately registered by the owning runtime are addressable.
- `describe` exposes stable identifiers and revisions.
- Actions are bounded by advertised schemas and capability policy.
- Cymonkey never scans scene trees, UObject heaps, DOMs, or UI trees to infer
  resources.
- Disconnecting never owns or terminates the caller's engine, process, display,
  placement, GPU, or credentials.

`player` is deliberately narrower: it represents safe media or game-session
semantics and never gives Cymonkey lifecycle control over the player. No
shipped runtime advertises player capabilities until a concrete, bounded
capability set is implemented.

## Migration status

The browser extension exposes `computer/browser-dom`; the macOS helper exposes
`computer/macos-app`; Three.js, Godot, Unity, and Unreal expose their
`render` runtimes. Cymonkey is the only semantic API.

For field-level migration and mixed-domain attachment examples, see
[Cymonkey domains, runtimes, and drivers](cymonkey-domains.md).
