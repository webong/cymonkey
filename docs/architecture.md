# Cymonkey architecture

Cymonkey is the public operator, host, and extension boundary. Users work with
three domains: `display`, `content`, and `device`. Cymy, the operator API,
configuration files, plugin manifests, and process protocols use only these
Cymonkey contracts.

The executable composes reusable Go libraries behind domain adapters. Those
adapters convert Cymonkey requests and responses to each library's native
types. The libraries do not import Cymonkey and remain usable by other hosts.
Library names and native schemas are implementation details, except in the
developer documentation for people embedding a library directly.

Optional executables implement the public `display.engine@v1`,
`content.provider@v1`, or `device.provider@v1` plugin contract. See
[Cymonkey plugins](cymonkey-plugins.md).

The host boundary has two modes today: direct library calls from the Cymonkey
executable and supervised service processes from a `cymonkey.config/v1alpha1`
manifest. The `observe` command uses provider HTTP endpoints so it can reach a
long-running display session and a content backend. Neither mode
transfers target ownership to a library. See
[Cymonkey host and operator](cymonkey-host.md).

Unity, Unreal, Godot, Blender, and Three.js are Jangolova runtime libraries. Cymonkey
enters the caller-owned runtime and coordinates their use; Jangolova exposes
the bounded semantic operations while the engine keeps rendering and the
supervisor separately owns target and display lifecycle. See
[subsystem boundaries](subsystem-boundaries.md).

Jangolova is an independent interaction and presentation toolbox that Cymonkey
can host. External agents, IDEs, and applications own planning and decisions;
Xallet, a native host, or another operator owns the target runtimes with which
Jangolova interacts. Blockade can run in process or serve HTTP. In the
implemented image workflow, Cymonkey requests screenshots through Jangolova's
display interface and sends them to Blockade.

Interaction includes operating semantic browser/application interfaces and
requesting display-level pointer/keyboard actions. Presentation includes
creating and updating dynamic 2D/3D interfaces. Both use the common semantic
protocol while target runtime and display ownership remain external.

## Browser extension system

The internal display library implements the browser extension system. It owns backend
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

Userscripts are a privileged Cymonkey augmentation form. Cymonkey owns their semantic
lifecycle through `capabilities`, `describe`, `act`, and `events`. Cymonkey's
local store and Jangolova's CDP/BiDi attachment provide extension-free
registration. Integrators can use the browser helper's userscript APIs in their
own extensions, and the macOS helper supplies source-free catalog data.

`pkg/macos-helper` includes a reusable Swift catalog and managed-runtime
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
    ├─ display contract
    │   ├─ browser and desktop interaction
    │   └─ dynamic web and render presentation
    │       → caller-owned targets
    ├─ device contract
    │   ├─ native device SDKs and registered providers
    │   └─ virtual display/audio runtimes and display byte gateway
    └─ content contract
        ├─ local model backends
        └─ registered cloud/provider adapters
            → normalized evidence and interpretations
```

The composition edge maps these public contracts to the selected reusable
libraries. Nothing above that edge depends on a library's native protocol.

Jangolova supports Playwright, Puppeteer, CDP, WebDriver BiDi, Safari MCP,
caller-owned WebExtensions, desktop helpers, and render libraries. Blockade
accepts images from approved captures or other callers. Its content contract
also accepts text and audio when a capable model provider is installed.

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

Board owns:

- provider registration, device attachment, and capability/resource grants;
- mounted-drive and macOS keyboard providers;
- native display capture/input/recording, display readiness, permission
  onboarding, volume inspection/archive transfer, and endpoint health SDKs;
- virtual X11, Wayland, and PulseAudio runtime implementations;
- the authenticated VNC/RDP/SPICE WebSocket byte gateway.

The SDKs are connected through desktop, audio, storage, endpoint, and WebRTC
providers. Board sessions enforce grants, bound streams, and report lifecycle
events. Cymonkey loads a host-owned inventory and exposes persistent operator
sessions. See [Board device sessions](device-sessions.md) and the
[extraction inventory](device-sdk-extraction.md).

The target provider owns:

- Chromium, WebKit, Gecko, SpiderMonkey, Unity, Unreal, and other executable
  runtime processes;
- physical machines, VMs, OCI workloads, and their lifecycle;
- displays, windows, surfaces, profiles, devices, networks, ports, and secrets;
- deployment of display/audio runtimes, endpoint exposure, access policy, and
  session state, using Board's reusable implementations where selected.
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

The authenticated, provider-neutral `cymonkey.display/v1alpha1` HTTP API transports those
calls. The cooperative Unity bridge implements the same vocabulary.

The repository has one host and many guests. `src/` is the host: the
single `cymy` binary, its generic contracts, admission policy and adapters.
`pkg/` holds everything the host runs or builds on: the Jangolova, Board and Console
modules, the content and generation kits, the content providers, and the platform helpers and
products. The host never imports a guest; it reaches them through the ext
plugin system (`github.com/webong/ext/pkg/plugin-go/contract`). Until the cut is
complete, `src/guest_imports_test.go` fails on any new import of a `pkg/` Go
module and lists the ones that remain. `tests/` is the conformance and fixture
surface. Jangolova, Board, Console and the kits are separate Go modules
(`jangolova`, `board`, `console`, `contentkit`, `generationkit`) joined through `go.work` and
local replacements while the host still imports them.
Each library's plugin adapter selects the native executable contracts it understands;
ext checks package structure and integrity without enumerating libraries. Cymonkey owns the command handlers, shared plugin directory, and
public `cymonkey.*` wire contracts. Its composition adapters translate to the
native library contracts at the executable boundary.

The host manifest also uses a generic `services` map keyed by public contract
names such as `display.interaction` and `content.interpretation`. The internal
host validates and supervises these endpoints without library-specific fields.

Console is the standalone composition library for callable tools. It publishes
definitions, makes one explicitly requested model turn, and dispatches a
selected call implemented by Jangolova, the content kit, Board, MCP, or application
code. The caller owns approvals, tool selection, transcripts, and subsequent
turns. Console has no autonomous agent loop.

Blockade owns neutral generation, streaming, multimodal message, tool proposal,
and speech contracts. Its catalogs start empty and accept opaque provider/model
IDs through explicit registration. Provider clients, API dialects, endpoint
configuration, and credential selection live in optional external packages;
Blockade never hardcodes them. See [SDK model and tool composition](sdk-model-tools.md).

## Package direction

```text
src/main.go/                   single Cymonkey operator/interface binary
src/contentcli/                Cymonkey content command handlers
src/devicecli/                 Cymonkey device command handlers
src/internal/provider/         provider and MCP implementation
src/fixtures/native-bridge/    native bridge fixture implementation
pkg/contentkit/                content provider host role and contracts
pkg/generationkit/             generation, embedding and speech contracts
pkg/console/                   standalone tool composition library
src/adapters/browserautomation/ Playwright CDP and Puppeteer CDP/BiDi attachment
src/adapters/webdriverclassic/  existing W3C WebDriver session attachment
src/adapters/safarimcp/         caller-owned Safari MCP relay attachment
src/adapters/displayinteraction/ provider-neutral VNC/WebRTC/Wayland interaction
src/adapters/webpresentation/   declarative web presentation adapter
src/internal/engineprovider/    target-in / semantic-call protocol
src/internal/orchestrator/      interaction lifecycle and target contracts
src/internal/bridge/            engine-neutral semantic methods
src/display_*.go                Executable-side display library services and adapter registration
src/observation/                Jangolova capture and Blockade inference workflow
pkg/board/                      standalone Board device library
pkg/board/drive.go              read-only mounted-drive provider
pkg/board/macoskeyboard/         explicit-PID macOS keyboard provider
pkg/board/host/                 provider registration and grant selection
src/internal/core/              core registry/composition/conformance code
src/internal/host/              Cymonkey host configuration and process supervisor
pkg/jangolova/                  standalone Jangolova library and host boundary
pkg/jangolova/host/             Jangolova host service injection contract
pkg/contentkit/                 content provider host role and contracts
pkg/generationkit/              generation, embedding and speech contracts
pkg/contentkit/protocol/          content contract schemas and fixtures
src/internal/targetconn/        caller-owned target connection helpers
pkg/jangolova/contract/         public runtime wire contract
pkg/jangolova/sdk/              public types and host-injection interfaces
pkg/jangolova/registry/         reviewed module discovery and activation
src/protocol/cymonkey/          canonical versioned Cymonkey schemas
src/protocol/browser-extension/ schema, recorded exchanges, and generated binding source
tests/cymonkey-core-conformance.mjs  portable Cymonkey contract checks
pkg/browser-helper/             browser API, extension management, and userscript library
pkg/macos-helper/       Swift native binding, userscript catalog, and managed-runtime controller
pkg/threejs/            explicit-registration Three.js runtime
pkg/                             distributable Godot, Unity, and Unreal Cymonkey packages
tests/godot-cymonkey-fixture/    license-free Godot conformance project
tests/unreal-cymonkey-fixture/   caller-owned Unreal conformance project
scripts/deploy/engine-runtime/     optional interaction artifact
scripts/deploy/godot-cymonkey/ optional headless Godot target image
scripts/deploy/unreal-cymonkey/ optional packaged Unreal target image
tests/docker/                    target-owning portability fixture only
```

No package imports Xallet. No product adapter provisions a target runtime.
The complete interface model is documented in
[Interface creation and operation](interface-model.md). Cymonkey's portable
contract and profile boundary are documented in
[Cymonkey runtime-agnostic augmentation](cymonkey-runtime.md).
