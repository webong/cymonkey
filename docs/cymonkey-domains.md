# Cymonkey interaction domains

Cymonkey describes **where an action takes effect**, not the product, process,
or transport used to reach it. Its three domains are:

| Domain | Meaning | Examples |
| --- | --- | --- |
| `viewer` | User-facing platform and application interfaces. | A desktop window, menus, Accessibility elements, keyboard and pointer interactions, browser tab control, storage, and network rules. |
| `render` | Visual composition and display semantics. | A browser document and its DOM, overlays, scripts, styles; a Three.js scene in a browser canvas; Unity, Unreal, or Godot scene, camera, material, object, animation, and viewport resources. |
| `player` | The semantic state and controls of a running content session. | A media player, browser game, Unity player, Unreal game session, play/pause/seek/load/session actions. |

`computer` is not a protocol domain. It is the caller-owned host on which one
or more of these domains may be available: a window may expose `viewer`, a
browser document or scene may expose `render`, and a running game or media
session may expose `player`.

`player` never grants process ownership. Starting, stopping, placing, or
allocating a player remains the responsibility of the caller-owned target
provider. A player capability is advertised only when the runtime exposes a
typed, bounded player operation.

## Domain, runtime, surface, resource, and driver

- A **target** is the caller-owned thing to which Jangolova attaches.
- A **domain** says which interaction plane an operation belongs to.
- A **runtime** identifies the concrete implementation within that plane, such
  as `browser-dom`, `macos-app`, `threejs`, `godot`, `unity`, or `unreal`.
- A **surface** is the augmentable area: document, window, canvas, scene, or
  viewport.
- A **resource** is an explicitly exposed item on a surface, such as a DOM
  summary, UI element, object, camera, material, animation, or player session.
- A **driver** is the Jangolova connection mechanism: CDP, WebDriver BiDi,
  WebExtension, Safari MCP, macOS Accessibility, Apple Events, or an
  authenticated Cymonkey WebSocket.

Runtimes are not domains. Chrome, Firefox, Safari, Three.js, Unity, and Unreal
can be part of the same target without becoming competing top-level domains.

## Mixed-domain targets

One target may expose more than one domain. For example, a browser tab that
contains a Three.js game can report:

```text
viewer domain
└── runtime: browser-dom
    └── surface: window:main

render domain
└── runtime: browser-dom
    └── surface: document:main

render domain
└── runtime: threejs
    └── surface: scene:main (inside document:main)

player domain
└── runtime: browser-game
    └── surface: player:main
```

The same goal may deliberately span them: query a document in `render`, move
a camera in `render`, then start a game session in `player`. Every advertised
capability carries its domain, runtime, driver, and applicable resource kinds,
so the caller can select an action without guessing from a browser, engine, or
operating-system brand.

An attachment may expose only the domains it can safely negotiate. Existing
browser implementations currently expose both `viewer` and `render`; current
engine packages expose `render`. `player` is part of the contract vocabulary now, but no
runtime claims it until it implements bounded player capabilities.

## Composite attachments

A Cymonkey attachment can combine independently caller-owned bindings. Each
binding negotiates its own `hello`, capabilities, surfaces, and events; the
composite exposes their union under one five-operation contract. It never
starts, embeds, or supervises any binding.

`act` carries optional `domain` and `runtime` selectors beside its capability
name. They are required only when the composite advertises the same capability
name from more than one binding. This makes a cross-domain workflow explicit:
the caller first queries `render/browser-dom`, then selects
`render/threejs` for a camera action. The coordinator does not turn those
steps into an implicit transaction or grant one domain authority over another.

### First public composite: macOS + Godot

The first configured composite is deliberately limited to a caller-owned macOS
application plus a caller-owned Godot scene. The root target is the macOS
application; its cooperative helper receives one-time launch credentials from
Jangolova. The Godot binding supplies its own already-running authenticated
`websocket` endpoint. Neither side is discovered, started, or stopped by
Jangolova.

```json
{
  "adapter": "cymonkey",
  "options": {
    "policy": {
      "allowedBundleIds": ["com.example.Player"],
      "allowedCapabilities": ["app.command.invoke", "object.visible.set"]
    },
    "composite": {
      "bindings": [
        { "id": "desktop", "module": "viewer.macos" },
        {
          "id": "scene",
          "module": "render.godot",
          "target": {
            "kind": "godot",
            "endpoints": [
              { "protocol": "websocket", "url": "ws://127.0.0.1:9321" }
            ]
          }
        }
      ]
    }
  },
  "target": { "kind": "macos-application" }
}
```

This is now expressed using the module IDs `viewer.macos` and
`render.godot`; see [Cymonkey runtime and driver modules](cymonkey-modules.md)
for the open composite shape and contributor contract.

## Wire vocabulary

The runtime-agnostic `jangolova.cymonkey/v1alpha2` contract uses:

```json
{
  "domains": ["viewer", "render"],
  "runtimes": ["browser-dom", "example.render"],
  "drivers": ["example-driver"]
}
```

Capabilities, surfaces, events, augmentation summaries, and augmentation
targets use `domain`; capability and surface descriptors also identify their
`runtime` and `driver` where applicable. `profile`, `web`, `macos`, and
`engine` are retired from the runtime-agnostic contract. Runtime and driver
names are open, syntax-validated identifiers; their safe meaning comes from a
registered Cymonkey module and negotiated capabilities, not a closed enum.

## Migration mapping

| Previous v1alpha2 vocabulary | Current vocabulary |
| --- | --- |
| `profile: web` | `domain: render` for document/overlay/script/style work; `domain: viewer` for window/platform work; `runtime: browser-dom` |
| `profile: macos` | `domain: viewer`, `runtime: macos-app` |
| `profile: engine` | `domain: render`, runtime is `threejs`, `godot`, `unity`, or `unreal` |
| `backend` | `driver` |
| `targetKinds` | `resourceKinds` |

The protocol methods remain unchanged: `hello`, `capabilities`, `describe`,
`act`, and `events`.
