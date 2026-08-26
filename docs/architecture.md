# Architecture

Unity and Unreal semantic presentation use the [Pacman boundary](pacman.md):
Jangolova dials a caller-owned semantic endpoint while the engine keeps
rendering and the supervisor separately owns target and display lifecycle.

Jangolova owns its Grimlock agent subsystem plus interaction and presentation
engines. Xallet, a native host, or another operator owns the target runtimes
with which those engines interact.

Interaction includes operating semantic browser/application interfaces and
requesting display-level pointer/keyboard actions. Presentation includes
creating and updating dynamic 2D/3D interfaces. Both use the common semantic
protocol while target runtime and display ownership remain external.

## Jangolova Browser Extension System

Jangolova is the browser runtime and extension product. It owns backend
discovery across CDP, WebDriver BiDi, Safari MCP, and WebExtension, plus the
authenticated control plane, browser policy, packaged injection, scoped
storage, network rules, and shared event service.

Its private extension control plane authenticates transport identity and then
separately authorizes every operation by capability, effect, target origin/tab,
and augmentation. Xallet Spook, optional caller-configured outbound WebSocket,
and extension-origin/CDP calls share this gate and its redacted audit stream.

Cymonkey is its runtime-agnostic augmentation subsystem. Its portable
`jangolova.cymonkey/v1alpha2` core owns augmentation lifecycle, surface
discovery, overlays, and capability negotiation. Runtime profiles add bounded
vocabularies: the web profile adds DOM/style/script operations; the macOS
profile maps allowlisted Apple Events and Accessibility operations to typed
`app.command.*` and `ui.*` capabilities. Jangolova owns each runtime backend,
its authenticated transport, consent checks, and policy.

Pacman remains the engine-neutral explicit-registration presentation contract.
In browsers, `@jangolova/threejs-pacman` maps allowlisted stable IDs to Three.js
scenes, objects, cameras, materials, and animation actions. It never scans
arbitrary page or scene objects.

Userscripts are a privileged Cymonkey augmentation form with the shared
`jangolova.cymonkey.userscript/v1alpha1` manifest. Cymonkey owns their semantic
lifecycle through `capabilities`, `describe`, `act`, and `events`. The WXT
extension provides the approval, storage, reconciliation, and native
registration manager. The macOS containing app receives source-free catalog
metadata from its embedded Safari extension; source never crosses that bridge.

`pkg/macos-ext` is the user-facing macOS product. It imports the distinct
`CymonkeyMacOSRuntime` library, can supervise that runtime when explicitly
started by the user, presents consent/runtime state in a menu bar, and embeds
the Safari WebExtension. It owns only its helper connection, never the target
applications it augments.

The public page bridge contains only page-safe Cymonkey operations. Platform
services and Pacman control are reachable only through the authenticated
extension control plane or a caller-owned CDP/BiDi/MCP connection.

## System boundary

```text
User, agent, IDE, or application
        |
        +-- deterministic engine API -----------------+
        |                                             |
        +-- HTTP / MCP / ACP --> Grimlock ------------+
                                      model + policy  |
                                                     v
                                CYMONKEY UNIFIED CONTROL PLANE
                                (jangolova.cymonkey/v1alpha2)
                                 hello / capabilities / describe / act / events
                                                     |
        +────────────────────────────────────────────┼────────────────────────────────────────────+
        │                                            │                                            │
        ▼                                            ▼                                            ▼
AUTOMATION DRIVERS                           INTERACTION DRIVERS                          PRESENTATION DRIVERS
• Playwright Driver (CDP)                    • WebExtension (WXT)                         • Three.js Pacman Driver
• Puppeteer Driver (CDP/BiDi)                • Userscripts Engine                         • Declarative Web Presentation
• Native CDP Driver                          • macOS Accessibility / Apple Events         • Unity / Unreal Bridge WS
• WebDriver BiDi Driver                      • Safari MCP Relay                           • Display Pixel / YOLO (Blockade)
        │                                            │                                            │
        └────────────────────────────────────────────┴────────────────────────────────────────────┘
                                                     │
                                                     v
                                           caller-owned targets
                                 (Xallet or native host owns lifecycle)
```

Endpoint and handle flow is inward: the operator creates a target and gives
Jangolova its connection coordinates. Jangolova never returns a newly created
Chromium endpoint because it does not create Chromium.

## Ownership

Jangolova owns:

- Cymonkey control plane and driver matrix (Playwright, Puppeteer, CDP, WebDriver BiDi, WebExtension, Safari MCP, macOS Accessibility);
- Playwright, Puppeteer, and browser automation drivers integrated into the Cymonkey control plane;
- WebDriver and MCP clients that attach to caller-owned WebKit/Safari targets;
- Three.js presentation logic, Pacman presentation drivers, and cooperative web experiences;
- Unity and Unreal interaction plugins and bridge protocol;
- semantic capability discovery, description, actions, observations, events,
  and interaction-session health;
- worker processes used internally by an interaction adapter.
- provider-neutral adapters that translate display observations and
  pointer/keyboard intent into a caller-supplied surface/input contract.

The target provider owns:

- Chromium, WebKit, Gecko, SpiderMonkey, Unity, Unreal, and other executable
  runtime processes;
- physical machines, VMs, OCI workloads, and their lifecycle;
- displays, windows, surfaces, profiles, devices, networks, ports, and secrets;
- VNC, WebRTC, CDP exposure, capture, access policy, and session state.
- browser-driver processes and any stdio/network relay required to expose a
  caller-owned Safari MCP server.

When Xallet is present, it is the target provider. In standalone use, a native
user or another system supplies the same target contract.

## Connection contract

An interaction adapter receives an adapter name, optional interaction-specific
options, and a caller-owned target containing:

- a target kind such as `browser` or `macos-application`;
- typed endpoints such as `cdp`, `webdriver-bidi`, `webdriver`,
  `mcp-streamable-http`, or `websocket`;
- optional opaque native handles.

Connecting creates only a Jangolova interaction session. Disconnecting releases
the adapter and its Playwright/Puppeteer worker without stopping the target.

## Semantic protocol

Every callable interaction engine uses the common bridge methods:

- `hello`
- `capabilities`
- `describe`
- `act`
- `events`

The authenticated, provider-neutral `interaction.engine/v1alpha1` HTTP API transports those
calls. The cooperative Unity bridge implements the same vocabulary.

## Package direction

```text
cmd/jangolova/              CLI and authenticated provider
internal/engineprovider/    target-in / semantic-call protocol
internal/orchestrator/      interaction lifecycle and target contracts
internal/bridge/            engine-neutral semantic methods
internal/grimlock/          ADK agents and caller-supplied model connectors
adapters/browserautomation/ Playwright CDP and Puppeteer CDP/BiDi attachment
internal/cymonkey/          runtime-agnostic augmentation contract and validation
adapters/cymonkey/          web backends plus bounded macOS capability mapping
adapters/webdriverclassic/  existing W3C WebDriver session attachment
adapters/safarimcp/         caller-owned Safari MCP relay attachment
adapters/displayinteraction/ provider-neutral VNC/WebRTC/Wayland display interaction
pkg/browser-ext/           single-build WXT runtime with optional Xallet Spook activation
pkg/macos-cymonkey-helper/  caller-owned Swift Apple Events/Accessibility binding
pkg/macos-ext/              menu-bar host, managed helper mode, and Safari container
pkg/userscript-runtime/     shared Cymonkey userscript validation and registration planning
protocol/userscript/        versioned Cymonkey userscript payload schema
protocol/browser-extension/ schema, recorded exchanges, and generated binding source
internal/browserextensionprotocol/ generated Go browser-extension bindings
pkg/threejs-cymonkey/         explicit-registration Three.js Pacman runtime
pkg/                        distributable Godot, Unity, and Unreal Pacman packages
tests/godot-cymonkey-fixture/ license-free Godot conformance project
tests/unreal-pacman-fixture/ caller-owned Unreal conformance project
deploy/engine-runtime/      optional interaction artifact
deploy/godot-cymonkey-fixture/ optional headless Godot target image
deploy/unreal-cymonkey-fixture/ optional packaged Unreal target image
tests/docker/               target-owning portability fixture only
```

No package imports Xallet. No product adapter provisions a target runtime.
The complete interface model is documented in
[Interface creation and operation](interface-model.md). Cymonkey's portable
contract and profile boundary are documented in
[Cymonkey runtime-agnostic augmentation](cymonkey-runtime.md).
