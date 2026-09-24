# Jangolova

Jangolova is a standalone Go module and runtime library suite for interacting
with and presenting inside dynamic software. It attaches to an existing target,
negotiates the capabilities that target exposes, and executes only those
semantic operations that policy allows.

It is designed for browser automation, application control, and interactive
presentation without taking ownership of the browser, application, renderer,
or user session.

## Capabilities

- Direct HTTP and MCP tool surfaces for agents and applications.
- Browser interaction through CDP, WebDriver BiDi, WebDriver Classic, Safari
  MCP, WebExtension, and native integrations.
- Dynamic presentation through explicit Three.js, Godot, Unity, Unreal, and Blender
  runtime libraries.
- Capability negotiation, action policy, owner approvals, audit events,
  connection recovery, and credential rotation.
- Explicit resource registration: scene and application libraries operate on
  stable IDs and allowlisted actions, never by scanning arbitrary objects.

## Quick start

Run the authenticated interaction provider:

```bash
go build -o .cache/bin/cymonkey ./src

export JANGOLOVA_PROVIDER_TOKEN="replace-with-a-random-secret"
.cache/bin/cymonkey provider serve-engine-provider --bind 127.0.0.1:7391
```

Connect a caller-owned target, then call its negotiated semantic methods through
`POST /v1/instances/{instanceId}/call`.

For an agent-facing tool server, run MCP over stdio or Streamable HTTP:

```bash
export JANGOLOVA_PROVIDER_TOKEN="replace-with-a-random-secret"
.cache/bin/cymonkey provider serve-mcp
.cache/bin/cymonkey provider serve-mcp --bind 127.0.0.1:7393
```

## Runtime libraries

The module namespace is `jangolova`; public imports use paths such as
`jangolova/sdk` and `jangolova/contract`.

Jangolova libraries run inside or beside the target runtime and translate
semantic requests into safe, native operations.

The `browserextension` package owns browser-native extension inspection,
deterministic ZIP packaging, staging, and guided installation. Its Chrome,
Chromium, and Edge adapter reports `installed` only after the browser's native
Load unpacked action survives a restart. Cymonkey exposes these adapters
through its `extension` host command
and owns caller policy and lifecycle semantics. See
[browser extension installation](../../docs/browser-extension-installation.md).

- **Browser and desktop** — document, window, display, and application actions.
- **Three.js** — registered scene, camera, material, and object operations.
- **Godot, Unity, Unreal, and Blender** — registered engine resources over authenticated
  cooperative transports.

The canonical module IDs are `render/threejs`, `render/godot`, `render/unity`,
`render/unreal`, `render/blender`, and `render/snapchat-camera-kit`. Their existing `pkg/*-cymonkey` directories are retained
as compatibility distribution paths; see the [render module ownership map](../../docs/jangolova-render-modules.md).

## Operating model

Jangolova receives target endpoints and credentials from its caller. It does
not launch targets, store their credentials, or decide which actions to take.
A caller supplies the target; Jangolova supplies the controlled interaction and
presentation capability.

## Documentation

- [Interaction provider](../../docs/engine-provider.md)
- [Bridge protocol](../../docs/bridge-protocol.md)
- [Three.js library](../../pkg/threejs-cymonkey/README.md)
- [Target connection security](../../docs/target-connection-security.md)

## Public integration boundary

The public wire types live in `contract/`; host-facing types and service ports
live in `sdk/`. `lib/jangolova` owns runtime selection and semantics without
private Cymonkey dependencies. The Jangolova host boundary is `host/`; Cymonkey
injects its private services through `src/internal/hostbinding`.

Cymonkey owns policy, approval, and coordination. Blockade owns visual inference
and cloud/VLM adapters. Jangolova never calls or configures Blockade.

See the [SDK guide](sdk/README.md) and [runtime validation record](../../docs/jangolova-runtime-validation.md).
