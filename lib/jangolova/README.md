# Jangolova

Jangolova is a standalone Go library suite for display interfaces: interacting
with and presenting inside dynamic software. It attaches to an existing
target, negotiates the capabilities that target exposes, and executes only
semantic operations allowed by its caller.

It is designed for browser automation, application control, and interactive
presentation without taking ownership of the browser, application, renderer,
or user session.

## Capabilities

- Host-injectable adapters for HTTP and MCP tool surfaces.
- Browser interaction through CDP, WebDriver BiDi, WebDriver Classic, Safari
  MCP, WebExtension, and native integrations.
- Dynamic presentation through explicit Three.js, Godot, Unity, Unreal, and Blender
  runtime libraries.
- Capability negotiation, action policy, owner approvals, audit events,
  connection recovery, and credential rotation.
- Explicit resource registration: scene and application libraries operate on
  stable IDs and allowlisted actions, never by scanning arbitrary objects.

## Use the library

Import `jangolova`, `jangolova/sdk`, and `jangolova/contract` in a Go host.
Supply the target, worker and transport services, and policy limits through
the public SDK. The host chooses its own executable, HTTP or MCP surface, and
runtime lifecycle. Run the module tests with `go test ./...` from this directory.

## Runtime libraries

The module namespace is `jangolova`; public imports use paths such as
`jangolova/sdk` and `jangolova/contract`.

Jangolova libraries run inside or beside the target runtime and translate
semantic requests into safe, native operations.

The `browserextension` package discovers local browser targets and owns browser-native extension inspection,
deterministic ZIP packaging, staging, and guided installation. Its Chrome,
Chromium, and Edge adapter reports `installed` only after the browser's native
Load unpacked action survives a restart. It also provides Firefox signed-XPI
BiDi installation and Safari caller-owned app packaging and launch. Safari
enablement remains a native browser action. The calling host owns policy,
approval, and lifecycle semantics for these adapters.

- **Browser and desktop** — document, window, display, and application actions.
- **Three.js** — registered scene, camera, material, and object operations.
- **Godot, Unity, Unreal, and Blender** — registered engine resources over authenticated
  cooperative transports.

The canonical module IDs are `render/threejs`, `render/godot`, `render/unity`,
`render/unreal`, `render/blender`, and `render/snapchat-camera-kit`.

## Operating model

Jangolova receives target endpoints and credentials from its caller. It does
not launch targets, store their credentials, or decide which actions to take.
A caller supplies the target; Jangolova supplies the controlled interaction and
presentation capability.

## Public integration boundary

The public wire types live in `contract/`; host-facing types and service ports
live in `sdk/`. Jangolova owns runtime selection and display semantics. The
host supplies endpoints, credentials, worker paths, policy decisions, and
approval. No operator implementation is imported by this Go module.

See the [SDK guide](sdk/README.md).
