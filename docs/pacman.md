# Pacman semantic presentation bridge

> [!IMPORTANT]
> Deprecation Notice: `jangolova.pacman/v1alpha1` is merging into the unified Cymonkey control plane as `jangolova.cymonkey/v1alpha2` (engine profile). See [Cymonkey display plane merge plan](cymonkey-display-plane-merge.md) for details. Existing `pacman` wire calls continue to work via compatibility translation during migration.

Pacman is Jangolova's embedded semantic control plane for Godot, Unity, and Unreal.
It is not a renderer, game runtime, display server, streamer, launcher, or
target supervisor. Godot, Unity, and Unreal render their own applications. Xallet or
another caller owns placement, GPU, display/pixel transport, credentials,
network reachability, and application lifecycle.

The Jangolova `pacman` adapter only attaches to a caller-owned target endpoint,
exchanges semantic messages, and closes its own connection when detached.
Disconnect never asks the application to quit and never terminates a process,
container, VM, display, or stream.

## Version and operations

The protocol identifier is `jangolova.pacman/v1alpha1`. The canonical Go types
are in `internal/pacman`; the JSON Schema is
`protocol/pacman/v1/protocol.schema.json`. Every transport implements these
request/response operations:

| Method | Semantics |
| --- | --- |
| `hello` | Exact-version negotiation and Unity/Unreal implementation identity. |
| `capabilities` | Bounded, schema-described operations and their effects. |
| `describe` | Current allowlisted semantic resources and a revision. |
| `act` | Invoke one advertised action, optionally against one stable resource ID. |
| `events` | Cursor-based reads of explicitly registered event types. |
| `health` | Target bridge readiness without exposing process-control semantics. |

Requests use `{"id":1,"method":"describe","params":{}}`. Responses contain
the same `id` and exactly one of `result` or a structured `error`. Message
ordering and framing are transport concerns; method meaning is not.

## Stable IDs and explicit allowlisting

Nothing is exported merely because it is a Unity `GameObject`, Component,
Scene, or an Unreal Actor, UObject, Level, or Widget. A project author must
register each semantic resource and action explicitly. Resources use one of:

`scene`, `object`, `ui`, `camera`, `material`, `animation`, `timeline`,
`artifact`, or `event`.

IDs have the stable form `kind:project-defined-name`, for example
`scene:main-menu`, `object:hero`, `ui:hud/score`, `camera:cinematic.main`, or
`timeline:intro`. The prefix must match `kind`; runtime instance IDs, hierarchy
paths, array indexes, Unity instance IDs, Unreal object addresses, and generated
`(Clone)` names are invalid. IDs remain stable across observations and should
remain stable across compatible application builds.

Capabilities declare the resource kinds they can target and a JSON input
schema. Implementations reject actions that were not registered, targets that
were not allowlisted, kind mismatches, duplicate IDs, and malformed input.
Descriptions contain only registered resources. Event types and sources are
also registered; events are bounded, cursor-addressed observations, not a dump
of engine logs.

## Model-authored scene plans

Models do not send engine code or arbitrary scene-tree mutations. Jangolova can
accept a bounded scene plan described by
[`protocol/pacman/v1/scene-plan.schema.json`](../protocol/pacman/v1/scene-plan.schema.json).
A plan declares the stable resources it needs and an ordered list of Pacman
actions. Before execution Jangolova negotiates `hello`, reads `capabilities` and
`describe`, verifies every required resource and target kind, then executes only
the advertised actions in order.

For example, a model can describe a house choreography as:

```json
{
  "apiVersion": "jangolova.pacman.scene/v1alpha1",
  "name": "midnight-house",
  "requires": [{"id": "object:door", "kind": "object"}],
  "actions": [{
    "name": "object.visible.set",
    "targetId": "object:door",
    "input": {"visible": false}
  }]
}
```

The plan layer is intentionally declarative and engine-neutral. Engines still
own rendering, physics, input, and game logic; Pacman remains the explicit
semantic boundary. The Godot house fixture contains a complete example at
`tests/godot-pacman-fixture/house.scene-plan.json`, and its rendered test proves
that the same model plan changes both the scene frame and Pacman event stream.

## Transport bindings

Pacman methods, resource IDs, errors, events, and health are independent of
connection framing. Jangolova adapters bind those semantics through a
`Connector`; Unity binds them through `IPacmanTransportHost`. Adding a Unix
socket, Windows named pipe, framed TLS stream, or another native binding must
not change `jangolova.pacman/v1alpha1` method payloads.

The endpoint protocol names the binding. The initial binding is
`pacman-ws`; future bindings use distinct names such as `pacman-unix` or
`pacman-pipe` and can coexist in the generic target descriptor.

## Initial WebSocket binding

The first mapping is an authenticated WebSocket endpoint supplied in the
generic target descriptor:

```json
{
  "apiVersion": "interaction.target/v1alpha1",
  "targetId": "showroom-unity-42",
  "kind": "unity",
  "endpoints": [{
    "name": "semantic",
    "protocol": "pacman-ws",
    "url": "wss://unity-target.example/pacman",
    "credentialRef": "unity-pacman-session",
    "tlsRef": "unity-pacman-trust",
    "audience": "pacman"
  }]
}
```

The endpoint must use `ws` or `wss` and cannot contain user information.
Jangolova resolves opaque credential/TLS references immediately before dialing
and supplies headers only during the handshake. Prefer `wss` except for a
caller-owned local transport. Tokens, TLS material, and resolved headers must
never appear in manifests, URLs, protocol results, health messages, events, or
logs.

The target owns the listener and remains alive after the socket closes. A
supervisor may publish or tunnel the listener and independently provide display
streaming; neither is part of Pacman.

Credential headers authenticate the WebSocket handshake. When Jangolova
receives a newer credential lease, the `pacman-ws` connector establishes and
validates a replacement connection, atomically switches the interaction
instance, acknowledges the lease generation, and then closes the old socket.
It emits `pacman.connection.renewed` or a secret-free
`pacman.connection.renewal_failed` lifecycle event. TLS material is loaded into
the dialer at connection time; replacing TLS material likewise takes effect on
the replacement connection.

## Implementations

`pkg/threejs-pacman` is the browser implementation for explicitly registered
Three.js resources. The Jangolova Browser Extension carries its calls through a
private MAIN-world invocation; it never adds Pacman control to the public page
global and never scans a site's JavaScript or scene graph.

`pkg/godot-pacman` is the license-free reference implementation. It provides a
headless Godot 4 package with explicit Node registrations, authenticated
`pacman-ws`, and the shared six-method contract.

`pkg/unity-pacman` is the distributable Unity implementation. It uses serialized
registrations for allowlisting and dispatches semantic work on Unity's main
thread. `PacmanWebSocketHost` is one replaceable
`IPacmanTransportHost`; `PacmanBridge` has no WebSocket listener policy. The
shared contract deliberately avoids Unity-specific types.

`pkg/unreal-pacman` is the distributable Unreal Engine plugin. Its initial
runtime module implements the same six methods through an explicitly populated
UObject/Actor registry and game-thread dispatcher. Its `IPacmanTransportHost`
boundary is implemented by an authenticated `FPacmanWebSocketHost`; a
platform-specific listener supplies accepted WebSocket connections without
coupling semantic dispatch to listener or application-lifecycle policy.

The provider-side `pacman` adapter, transport connector, schema, and
conformance suite are shared by both engines. Unity is therefore attached
through the normal provider target descriptor today; it does not need a
separate Unity-specific Jangolova adapter. The remaining Unity work is transport
hardening and live platform coverage. The remaining Unreal work is the
platform-specific listener/upgrade binding and live packaged-game fixture.

The Godot fixture is at `tests/godot-pacman-fixture`; its headless container is
at `deploy/godot-pacman-fixture`. Godot is the license-free reference runtime,
while Unity and Unreal remain engine-specific integrations.

The source-only Unreal fixture is at
`tests/unreal-pacman-fixture`. Its optional packaged-runtime image definition
is at `deploy/unreal-pacman-fixture`; that environment is separate from the
Jangolova provider image and accepts operator-supplied Unreal build/runtime
images.

If a Pacman attachment fails, the provider follows the common
[interaction attachment recovery](attachment-recovery.md) policy and redials
the same caller-owned endpoint. It does not restart or replay actions into the
Unity or Unreal application.

## Compatibility notes

- The older `jangolova.bridge/v1alpha1` native socket direction is preserved
  but documented as legacy. Pacman uses generic target-descriptor attachment.
- The Unity WebSocket host remains an initial Unity 2022.3/.NET 4.x
  `HttpListener` binding. Platforms without that support can implement another
  `IPacmanTransportHost` without changing Pacman semantics.
- Unreal live compilation and packaged-runtime coverage require an approved
  Unreal Engine toolchain; the source package and fixture remain distributable
  without bundling the editor or engine binaries.
