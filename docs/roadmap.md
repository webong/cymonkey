# Roadmap

## Phase 0: Grimlock agent interface

- [x] Define Grimlock as Jangolova's internal model-powered agent subsystem,
  distinct from deterministic engine APIs and target lifecycle ownership.
- [x] Define a separate caller-supplied model profile with opaque credential
  and TLS references.
- [x] Add the ADK Go agent factory, model-connector registry, and initial
  OpenAI-compatible gateway connector.
- [x] Adapt Jangolova capabilities into effect-classified ADK tools with
  approval checks immediately before execution.
- [x] Add the native Grimlock HTTP session/run/event API.
- [x] Add the MCP adapter over the same Grimlock application service.
- [x] Add the ACP adapter over the same Grimlock application service.
- [ ] Add persistent agent sessions, budgets, tracing, and multi-agent
  workflows.

## Phase 1: Correct interaction boundary

- [x] Accept caller-owned target endpoints and opaque handles.
- [x] Remove Chromium, native-process, and display-runtime launch adapters.
- [x] Make disconnect target-preserving.
- [x] Add a repository boundary test against target provisioning.
- [x] Keep native-host, independent-container, and Xallet-managed operation.
- [x] Define a provider-neutral caller-supplied target descriptor.
- [x] Select interaction engines automatically from protocols and required
  capabilities without using target location.
- [x] Resolve opaque credential and TLS references into expiring,
  adapter-private connection material.
- [x] Verify authenticated remote CDP attachment, release, TLS trust, and
  provider-output redaction.

## Phase 2: Browser interaction engines

- [x] Add Playwright Core attachment over CDP.
- [x] Add Puppeteer Core attachment over CDP.
- [x] Expose `hello`, `capabilities`, `describe`, `act`, and `events` through
  the authenticated provider.
- [x] Add active connection health and lifecycle events.
- [x] Verify disconnect does not terminate caller-owned Chromium.
- [x] Add Puppeteer attachment over WebDriver BiDi and verify it against
  caller-owned Firefox.
- [x] Add target-preserving W3C WebDriver Classic attachment for existing
  Safari and other browser sessions.
- [x] Add a named WebKit WebDriver adapter and verify it against a caller-owned
  WebKitGTK session.
- [x] Add a Safari MCP client over a caller-owned Streamable HTTP relay with
  dynamic tool discovery.
- [ ] Add a Safari 27/STP live fixture when that runtime is available in CI.
- [ ] Add richer page observation and accessibility descriptions.

## Phase 2b: Augmented browsing

- [x] Define the Cymonkey page-safe and privileged-extension trust boundary.
- [x] Add the nested `window.jangolova.cymonkey` page bridge.
- [x] Define `jangolova.cymonkey/v1alpha1`, the augmentation schema, backend
  interface, auto-selection policy, and capability persistence metadata.
- [x] Add no-install CDP and first-class WebDriver BiDi mappings for the same
  augmentation contract.
- [x] Run the same reversible augmentation lifecycle against test-owned
  Chromium CDP and Firefox WebDriver BiDi targets while proving target-preserving disconnect.
- [x] Add dynamic Safari MCP mapping that advertises only explicitly supported
  script, preload, DOM, and network semantics.
- [x] Add packaged script registration/execution, CSS injection, extension
  storage, Shadow DOM overlays, and declarative network rules.
- [x] Detect an optional caller-installed Chromium extension through a
  caller-owned CDP target without launching or stopping the browser.
- [x] Add WXT Manifest V3 packaging for Chrome, Edge, and Firefox, including
  one standalone artifact with runtime-activated Xallet Spook integration.
- [ ] Add live cross-browser extension fixtures.
- [x] Add provider-level capability and origin filtering.
- [x] Add per-call extension policy across caller, capability, effect,
  origin/tab, and augmentation with redacted audit lifecycle events.
- [x] Add an optional extension-initiated authenticated WebSocket using a
  caller-supplied short-lived token in the same browser artifact.
- [ ] Add signed augmentation bundles.

## Phase 2c: Runtime-agnostic augmentation

- [x] Define `jangolova.cymonkey/v1alpha2` as a portable augmentation contract
  with typed runtime profiles, surfaces, lifecycle, capability provenance, and
  target-neutral manifests.
- [x] Adapt the Jangolova Browser Extension control plane to advertise the
  `web` profile while retaining the page-safe `v1alpha1` compatibility bridge.
- [x] Define a bounded macOS profile over typed `app.command.*` and `ui.*`
  operations without raw AppleScript, raw Apple Events, or unrestricted
  Accessibility-tree passthrough.
- [x] Add shared Go validation and a policy-filtered macOS capability mapper.
- [x] Implement a caller-owned macOS native helper that negotiates Automation
  and Accessibility consent and exposes only mapped capabilities.
- [x] Add a macOS menu-bar containing app that imports the reusable Cymonkey
  runtime, offers explicit managed start/stop, and embeds a Safari WebExtension.
- [x] Define Cymonkey userscript lifecycle capabilities and the shared
  `jangolova.cymonkey.userscript/v1alpha1` manifest, bounded
  `@grant none` MVP, approval checks, permission-increase checks, and native
  WebExtension registration adapter.
- [x] Share source-free userscript catalog metadata from the Safari extension
  to the containing app through an App Group native-message handler.
- [ ] Enable Safari userscript execution only when a shipping Safari runtime
  exposes and passes the dedicated native userscript capability probe.
- [ ] Run shared lifecycle conformance against web and fake/native macOS
  backends, including stale surface references and permission revocation.

## Phase 3: Presentation engines

- [x] Keep the Three.js dynamic-scene experience and protocol.
- [x] Keep the Unity package and authenticated native bridge.
- [x] Package a web presentation host behind the provider-visible
  `web-presentation` adapter (HTML/CSS/JS write plus
  create/replace/patch/describe/act/capture/events).
- [x] Add a live Chromium authored-presentation smoke test and verify
  target-preserving disconnect.
- [x] Define artifact size, origin, revision, and asset-loading policy.
- [x] Add provider-neutral artifact references and `presentation.mount`.
- [x] Verify artifact mounting in a direct-container target without Xallet.
- [x] Add authorization, audit, timeout, and cancellation hooks for authored
  JavaScript execution.
- [x] Define the Pacman Godot/Unity/Unreal semantic protocol and lifecycle boundary.
- [x] Add Godot as the license-free Pacman reference runtime.
- [x] Add a headless Godot 4 fixture and authenticated `pacman-ws` container.
- [x] Expose authenticated caller-owned `pacman-ws` attachment through the
  interaction provider.
- [x] Add a minimal Unity Pacman package with explicit resource/action
  allowlisting and target-preserving disconnect verification.
- [x] Scaffold a distributable Unreal plugin with explicit resource/action
  registration and game-thread semantic dispatch.
- [x] Add the authenticated Unreal WebSocket host and game-thread request
  router boundary.
- [x] Add a separate source-only Unreal fixture project and packaged-runtime
  image definition.
- [ ] Add a platform-specific Unreal listener/upgrade binding and live
  packaged-game conformance fixture.
- [ ] Harden and platform-test the Unity WebSocket listener beyond the .NET 4.x
  MVP transport.

## Phase 4: General display interaction

- [x] Define provider-neutral capture, coordinate-space, focus, pointer, and
  keyboard target contracts (`display-interaction` engine adapter).
- [x] Add display observation (`display.describe`, `display.capture`) and pointer/keyboard (`pointer.*`, `keyboard.*`) semantic capabilities.
- [x] Add policy metadata for coordinate clicks, typing, and sensitive input.
- [ ] Verify native-host and Xallet-provided display contracts with the same
  adapter conformance suite.

## Phase 4b: Blockade observation

- [x] Define the provider-neutral Blockade observation contract.
- [x] Add managed Python Ultralytics workers for local YOLO/SAM inference.
- [x] Add worker-pool lifecycle, framed stdin/stdout IPC, and shutdown.
- [x] Add YAML engine configuration and local model-file validation.
- [x] Add `blockade validate` and `blockade observe` CLI flows.
- [x] Expose Blockade observation as a read-only Grimlock tool.
- [x] Add Grimlock model roles for reasoning, vision, and multimodal models.
- [x] Define a Grimlock-owned vision-provider registry boundary.
- [x] Add reproducible model-cache mounts and a real YOLO/SAM fixture launcher.
- [x] Run the fixture with pinned weights and add a real inference smoke test.
- [x] Add native ONNX Runtime inference.
- [ ] Cloud-provider adapters (fal.ai and similar): community-owned via the
  Grimlock `VisionProvider` boundary, contract validators, and the
  `jangolova-blockade-cloud-provider` agent skill.
- [ ] Capture screenshots from Cymonkey, Pacman, and display targets for Blockade.
- [ ] Add the full screenshot → observation → Grimlock → approved-action test.

## Phase 5: Hardening

- [x] Add renewable credential leases for HTTP and long-lived CDP/BiDi
  interactions.
- [x] Add live CA and client-certificate transport rotation, with atomic HTTP
  transport promotion and process-safe CDP worker replacement.
- [x] Extend per-capability policy hooks and audit records beyond the current
  presentation execute/capture path.
- [x] Reattach failed engine instances to the same caller-owned target with
  bounded backoff and action-safe lifecycle events.
- [x] Add caller reconciliation fixtures for rebuilding desired interaction
  instances after the Jangolova provider process restarts.
- [x] Add generated TypeScript/Go browser-extension protocol bindings and
  recorded legacy/current compatibility fixtures.
- [x] Extend generated clients and compatibility fixtures to the remaining
  Jangolova protocols after their contracts stabilize.
- [ ] Sign and publish versioned interaction-runtime images.
