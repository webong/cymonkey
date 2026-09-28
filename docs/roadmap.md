# Roadmap

## Phase 0: Direct agent-tool interface

- [x] Define Jangolova as a target-attached capability toolbox, not an agent.
- [x] Keep planning, model selection, memory, and session ownership outside
  Jangolova.
- [x] Expose the authenticated Engine Provider HTTP API directly to callers.
- [x] Add a direct MCP tool server over the same Engine Provider operations.
- [x] Record requested, denied, failed, and completed action audit events at
  the execution boundary.
- [x] Add the standalone `blockade` executable and the initial `cymonkey`
  host that composes standalone Jangolova and Blockade processes.
- [ ] Add host-level component health, discovery, restart policy, and
  coordinated routing.

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

- [x] Add Jangolova bookmarklet URL import/export and draggable HTML installers
  through `cmy bookmarklet`, with reversible example and real Chrome/Firefox
  link-activation checks.
- [x] Serialize per-augmentation lifecycle operations and test action/cleanup
  races, cleanup retries, and invalid-runtime cleanup.
- [x] End Firefox BiDi automation sessions on detach and verify fresh attachment
  preserves the caller's browser/page and removes managed preloads.
- [x] Align Camera Kit's page-bound capability lifetime with the shared schema.
- [ ] Validate native bookmarks-bar installation and activation on Safari/Edge
  and mobile browsers; link fixtures do not certify browser-toolbar behavior.

- [x] Define the Cymonkey page-safe and privileged-extension trust boundary.
- [x] Add the nested `window.cymonkey.jangolova` page bridge.
- [x] Define `cymonkey/v1alpha1`, the augmentation schema, backend
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
- [x] Extract browser API and optional Xallet Spook integrations from the
  earlier WXT prototype into reusable libraries.
- [ ] Add live cross-browser extension fixtures.
- [x] Add provider-level capability and origin filtering.
- [x] Add per-call extension policy across caller, capability, effect,
  origin/tab, and augmentation with redacted audit lifecycle events.
- [x] Provide an optional extension-initiated authenticated WebSocket client
  using a caller-supplied short-lived token.
- [ ] Add signed augmentation bundles.

## Phase 2c: Runtime-agnostic augmentation

- [x] Define `cymonkey/v1alpha1` as a portable augmentation contract
  with typed domains, runtimes, drivers, surfaces, lifecycle, capability
  provenance, and target-neutral manifests.
- [x] Extract the browser extension control contract and browser API helpers
  into libraries for consuming extensions. Retain CDP/BiDi browser attachment.
- [x] Define a bounded `viewer` / `macos-app` mapping over typed `app.command.*` and `ui.*`
  operations without raw AppleScript, raw Apple Events, or unrestricted
  Accessibility-tree passthrough.
- [x] Add shared Go validation and a policy-filtered macOS capability mapper.
- [x] Implement a caller-owned macOS native helper that negotiates Automation
  and Accessibility consent and exposes only mapped capabilities.
- [x] Extract the macOS catalog and managed-runtime controller into a Swift
  library for caller-owned apps.
- [x] Define Cymonkey userscript lifecycle capabilities and the shared
  `cymonkey.userscript/v1alpha1` manifest, bounded
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
- [x] Define the Cymonkey render contract and lifecycle boundary for Godot, Unity, and Unreal.
- [x] Add Godot as the license-free Cymonkey render reference runtime.
- [x] Add a headless Godot 4 fixture and authenticated `websocket` container.
- [x] Expose authenticated caller-owned `websocket` attachment through the
  interaction provider.
- [x] Add a minimal Unity Cymonkey package with explicit resource/action
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

## Phase 4b: Blockade inference interfaces

- [x] Define the provider-neutral Blockade observation contract.
- [x] Add managed Python Ultralytics workers for local YOLO/SAM inference.
- [x] Add worker-pool lifecycle, framed stdin/stdout IPC, and shutdown.
- [x] Add YAML engine configuration and local model-file validation.
- [x] Add `cymonkey blockade validate` and `cymonkey blockade observe` CLI flows.
- [x] Keep Blockade as a read-only observation capability for external agents
  and applications.
- [x] Consolidate engine render control into Cymonkey's `render` domain
  (docs/cymonkey-display-plane-merge.md).
- [x] Add reproducible model-cache mounts and a real YOLO/SAM fixture launcher.
- [x] Run the fixture with pinned weights and add a real inference smoke test.
- [x] Add native ONNX Runtime inference.
- [x] Add ordered ONNX Runtime execution-provider configuration with explicit
  TensorRT, CUDA, OpenVINO, and Core ML adapters plus an implicit CPU fallback.
- [x] Make the ONNX smoke fixture execution-provider-selectable and validate a
  real Core ML session on Apple Silicon.
- [x] Move screenshot-to-observation coordination to Cymonkey: it calls
  Jangolova's normal screenshot action and then calls standalone Blockade.
- [x] Add the Blockade-owned provider-adapter registry and versioned observe,
  capabilities, and readiness contract with typed provider failures.
- [x] Add named provider-adapter YAML, bounded timeout/payload enforcement,
  environment-only secret references, unified inference selection, and a fake
  adapter integration suite.
- [ ] Validate TensorRT + CUDA fallback on NVIDIA hardware and document a
  reproducible engine/timing-cache fixture.
- [ ] Validate OpenVINO on Intel CPU/GPU/NPU hardware with a reproducible
  model-cache fixture.
- [ ] Benchmark Core ML CPU/GPU/Neural Engine placement and latency on the
  representative model set.
- [ ] Add the first real cloud/VLM package (fal.ai or similar) to Blockade's
  adapter registry and map its native response to the observation contract.
- [ ] Define Blockade's sound inference request, response, and capability
  contract before adding local or cloud sound adapters.
- [ ] Expose Cymonkey observation coordination as an authenticated operator API.
- [ ] Capture screenshots from additional Cymonkey and display targets for
  Blockade.
- [ ] Add the full screenshot → observation → external-agent decision →
  approved-action test.

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
