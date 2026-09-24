# Cymonkey architecture

Cymonkey is the operator, host, and extension boundary. Jangolova owns display
interfaces for interaction and presentation; Blockade owns inference
interfaces for image and sound across local and cloud backends. Cymonkey
composes and supervises their processes and coordinates cross-subsystem
workflows while preserving their standalone boundaries. Blockade's implemented
public request currently handles images; sound inference remains to be
defined. See
[Cymonkey naming migration](naming-migration.md).

The dependency direction is one way: Cymonkey imports the public Jangolova and
Blockade modules. Each library can run or be embedded with another host, and
neither imports Cymonkey. Cymonkey-specific worker discovery, approval, and
event compatibility live at the Cymonkey host boundary.

The host boundary is executable-oriented: it starts standalone `jangolova` and
`blockade` processes from a `cymonkey.config/v1alpha1` manifest. It may
coordinate their endpoints, but it does not embed their implementation or
transfer target ownership to either subsystem. See
[Cymonkey host and operator](cymonkey-host.md).

Unity, Unreal, Godot, Blender, and Three.js are Jangolova runtime libraries. Cymonkey
enters the caller-owned runtime and coordinates their use; Jangolova exposes
the bounded semantic operations while the engine keeps rendering and the
supervisor separately owns target and display lifecycle. See
[subsystem boundaries](subsystem-boundaries.md).

Jangolova is an independent interaction and presentation toolbox that Cymonkey
can host. External agents, IDEs, and applications own planning and decisions;
Xallet, a native host, or another operator owns the target runtimes with which
Jangolova interacts. Blockade is the separate inference service. In the
implemented image workflow, Cymonkey requests screenshots through Jangolova's
display interface and sends them to Blockade.

Interaction includes operating semantic browser/application interfaces and
requesting display-level pointer/keyboard actions. Presentation includes
creating and updating dynamic 2D/3D interfaces. Both use the common semantic
protocol while target runtime and display ownership remain external.

## Jangolova Browser Extension System

Jangolova is the browser extension system. It owns backend
discovery across CDP, WebDriver BiDi, Safari MCP, and WebExtension, plus the
authenticated control plane, browser policy, packaged injection, scoped
storage, network rules, and shared event service.

Its private extension control plane authenticates transport identity and then
separately authorizes every operation by capability, effect, target origin/tab,
and augmentation. Xallet Spook, optional caller-configured outbound WebSocket,
and extension-origin/CDP calls share this gate and its redacted audit stream.

Cymonkey is the runtime-entry and augmentation subsystem: it gains approved
access to a target, mounts reviewed packages, and routes authenticated calls.
Jangolova owns each runtime library and adapter, including its semantic scene
operations and authenticated transport. The calling host supplies approval
and policy limits. Every
capability identifies a domain (`viewer`, `render`, or `player`), concrete
runtime, and driver. The browser and macOS application mappings operate in
`viewer`; explicitly registered Three.js, Godot, Unity, Unreal, and Blender resources
operate in `render`. `player` reserves lifecycle-safe media/game-session
semantics.

The render domain remains explicit-registration only. In browsers,
`@jangolova/threejs-cymonkey` maps allowlisted stable IDs to Three.js scenes,
objects, cameras, materials, and animation actions. It never scans arbitrary
page or scene objects.

Userscripts are a privileged Cymonkey augmentation form with the shared
`jangolova.cymonkey.userscript/v1alpha1` manifest. Cymonkey owns their semantic
lifecycle through `capabilities`, `describe`, `act`, and `events`. Cymonkey's
local store and Jangolova's CDP/BiDi attachment provide extension-free
registration. Integrators can use the shared userscript runtime in their own
extensions, and the macOS browser adapter supplies source-free catalog data.

`pkg/macos-browser-adapter` is a reusable Swift catalog and managed-runtime
controller. A caller-owned macOS app can import it and provide its own UI and
Safari extension if desired. It owns only its helper connection, never the
target applications it augments.

The public page bridge contains only page-safe Cymonkey operations. Platform
services and render control are reachable only through the authenticated
extension control plane or a caller-owned CDP/BiDi/MCP connection.

## System boundary

```text
External agent, IDE, or application
  → Cymonkey entry, policy, approval, and module composition
    ├─ Jangolova display interfaces
    │   ├─ browser and desktop interaction
    │   └─ dynamic web and render presentation
    │       → caller-owned targets
    └─ Blockade inference interfaces
        ├─ local model backends
        └─ registered cloud/provider adapters
            → normalized inference evidence
```

Jangolova supports Playwright, Puppeteer, CDP, WebDriver BiDi, Safari MCP,
caller-owned WebExtensions, desktop helpers, and render libraries. Blockade
currently accepts images from approved captures or other callers. Sound
inference belongs to its boundary once its request contract is defined.

Endpoint and handle flow is inward: the operator creates a target and gives
Jangolova its connection coordinates. Jangolova never returns a newly created
Chromium endpoint because it does not create Chromium.

## Ownership

Jangolova owns:

- its display-interface driver matrix (Playwright, Puppeteer, CDP, WebDriver BiDi, WebExtension, Safari MCP, macOS Accessibility);
- Playwright, Puppeteer, and browser automation adapters mapped into the Cymonkey contract;
- WebDriver and MCP clients that attach to caller-owned WebKit/Safari targets;
- Three.js render logic and cooperative web experiences;
- Godot, Unity, Unreal, and Blender render modules and bridge protocols;
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

The repository uses `src/` for Cymonkey provider code, generic contracts,
adapters, and protocol assets. Standalone Jangolova and Blockade libraries live
under root `lib/`; `src/main.go` is the single Cymonkey interface binary. `pkg/`
remains the distribution surface for runtime packages and platform products,
while `tests/` remains the conformance and fixture surface. The Jangolova and Blockade
libraries are separate Go modules (`jangolova` and `blockade`) joined to the
Cymonkey module through `go.work` and local replacements.

## Package direction

```text
src/main.go/                   single Cymonkey operator/interface binary
src/internal/provider/         provider and MCP implementation
src/fixtures/native-bridge/    native bridge fixture implementation
lib/blockade/                  standalone Blockade inference library
src/adapters/browserautomation/ Playwright CDP and Puppeteer CDP/BiDi attachment
src/adapters/webdriverclassic/  existing W3C WebDriver session attachment
src/adapters/safarimcp/         caller-owned Safari MCP relay attachment
src/adapters/displayinteraction/ provider-neutral VNC/WebRTC/Wayland interaction
src/adapters/webpresentation/   declarative web presentation adapter
src/internal/engineprovider/    target-in / semantic-call protocol
src/internal/orchestrator/      interaction lifecycle and target contracts
src/internal/bridge/            engine-neutral semantic methods
src/internal/builtin/           built-in engine registration
src/internal/hostbinding/       Cymonkey binding into the Jangolova host boundary
src/internal/core/              core registry/composition/conformance code
src/internal/host/              Cymonkey host supervisor and observation coordinator
lib/jangolova/                  standalone Jangolova library and host boundary
lib/jangolova/host/             Jangolova host service injection contract
lib/blockade/                   standalone Blockade inference library
lib/blockade/protocol/          Blockade schemas and fixtures
src/internal/targetconn/        caller-owned target connection helpers
lib/jangolova/contract/         public runtime wire contract
lib/jangolova/sdk/              public types and host-injection interfaces
lib/jangolova/registry/         reviewed module discovery and activation
src/protocol/cymonkey/          canonical versioned Cymonkey schemas
src/protocol/browser-extension/ schema, recorded exchanges, and generated binding source
tests/cymonkey-core-conformance.mjs  portable Cymonkey contract checks
pkg/browser-adapter/            composable browser API, policy, and package library
pkg/extension-manager/         management API interface with optional host installation adapter
pkg/macos-cymonkey-helper/       caller-owned Swift Apple Events/Accessibility binding
pkg/macos-browser-adapter/      reusable Swift catalog and managed-runtime controller
pkg/userscript-runtime/          shared userscript validation and registration planning
pkg/threejs-cymonkey/            explicit-registration Three.js runtime
pkg/                             distributable Godot, Unity, and Unreal Cymonkey packages
tests/godot-cymonkey-fixture/    license-free Godot conformance project
tests/unreal-cymonkey-fixture/   caller-owned Unreal conformance project
infra/deploy/engine-runtime/     optional interaction artifact
infra/deploy/godot-cymonkey-fixture/ optional headless Godot target image
infra/deploy/unreal-cymonkey-fixture/ optional packaged Unreal target image
tests/docker/                    target-owning portability fixture only
```

No package imports Xallet. No product adapter provisions a target runtime.
The complete interface model is documented in
[Interface creation and operation](interface-model.md). Cymonkey's portable
contract and profile boundary are documented in
[Cymonkey runtime-agnostic augmentation](cymonkey-runtime.md).
