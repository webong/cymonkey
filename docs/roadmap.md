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
- [x] Add a loopback authenticated operator surface for browser selection,
  persistent interaction calls, userscripts, extensions, and image observation.

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
  through `cymy bookmarklet`, with reversible example and real Chrome/Firefox
  link-activation checks.
- [x] Serialize per-augmentation lifecycle operations and test action/cleanup
  races, cleanup retries, and invalid-runtime cleanup.
- [x] End Firefox BiDi automation sessions on detach and verify fresh attachment
  preserves the caller's browser/page and removes managed preloads.
- [x] Align Camera Kit's page-bound capability lifetime with the shared schema.
- [ ] Validate native bookmarks-bar installation and activation on Safari/Edge
  and mobile browsers; link fixtures do not certify browser-toolbar behavior.

- [x] Define the Cymonkey page-safe and privileged-extension trust boundary.
- [x] Add the nested `window.cymonkey.display` page bridge.
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

## Phase 4b: Blockade content interpretation

- [x] Define the provider-neutral Blockade image observation contract.
- [x] Add a standalone Ultralytics source provider for local YOLO/SAM inference.
- [x] Make Blockade own managed Python installation, worker-pool lifecycle,
  framed stdin/stdout IPC, and shutdown.
- [x] Add provider-only YAML configuration and installed-model validation.
- [x] Add `cymy content validate` and `cymy content observe` CLI flows.
- [x] Keep Blockade as a read-only observation capability for external agents
  and applications.
- [x] Consolidate engine render control into Cymonkey's `render` domain
  (docs/cymonkey-display-plane-merge.md).
- [x] Add reproducible model-cache mounts and a real YOLO/SAM fixture launcher.
- [x] Run the fixture with pinned weights and add a real inference smoke test.
- [x] Add native ONNX Runtime inference as a standalone executable provider.
- [x] Add ordered ONNX Runtime execution-provider configuration with explicit
  TensorRT, CUDA, OpenVINO, and Core ML adapters plus an implicit CPU fallback.
- [x] Make the ONNX smoke fixture execution-provider-selectable and validate a
  real Core ML session on Apple Silicon.
- [x] Move screenshot-to-observation coordination to Cymonkey: it calls
  Jangolova's normal screenshot action and then calls the Blockade service.
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
- [x] Define Blockade's separate text, image, and audio content request,
  interpretation response, capability, and multimodal composition contract.
- [x] Route the content contract through Blockade's HTTP and executable
  provider adapter path while retaining the image observation API.
- [ ] Add a production text model provider package and validate its native
  response mapping against the content contract.
- [ ] Add a production sound model provider package for transcription and/or
  sound-event interpretation.
- [x] Define a model registration and installation contract separate from
  provider installation, including artifact digests, associated files,
  capability declarations, and provider compatibility checks.
- [x] Add a Blockade model installer and registry for local artifacts; support
  remote model identifiers for cloud providers without downloading weights.
- [x] Extract Ultralytics, ONNX, and WebLLM into standalone provider packages
  behind the same contract; support both executable and Blockade-run source
  packages, and remove the direct Python HTTP integration.
- [x] Separate staged model inspection from long lived inference startup and
  add model list and removal lifecycle commands.
- [ ] Verify one local and one cloud provider through the same contract and
  normalized result tests.
- [x] Expose the current image observation coordination through an
  authenticated Cymonkey operator API.
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

## Device access across macOS, Windows, iOS and Android

Planned work extending the extracted Board mechanics. Platform implementations
remain in Board; the viewer and public integration surface belong to Cymonkey.
Xallet consumes those public contracts and retains its policy and deployment
lifecycle. These tasks are not claims of current end-to-end platform support.

### Native audio backends

- [ ] Add macOS system-audio capture, microphone capture, and playback through
  native platform facilities, without requiring PulseAudio.
- [ ] Add Windows system-audio capture, microphone capture, and playback through
  native platform facilities, without requiring PulseAudio.
- [ ] Expose host-approved audio device selection and supported formats through
  the device contract. Advertise only operations available on the selected host.
- [ ] Enforce independent grants for system audio, microphone, and playback.
  Surface missing permissions and handle revocation or device removal.
- [ ] Connect both native backends to the common WebRTC media interface, including
  format conversion, bounded buffers, and cleanup on session close.
- [ ] Add Opus, bounded jitter buffering, and packet-loss handling to the shared
  audio transport; retain explicit codec negotiation and interoperability checks.

Acceptance: send a known test tone in each authorized direction; prove denied
microphone/playback grants prevent access; disconnect and revoke during active
media; verify all owned audio streams stop. Native macOS/Windows audio remains
pending until these checks pass on the corresponding OS.

### Remote viewer

- [ ] Add a Cymonkey viewer client using the public device-session contract for
  attachment, SDP/ICE exchange, video rendering, and audio playback.
- [ ] Add explicit viewer controls for keyboard control and microphone sharing.
  Keep view-only sessions view-only; show granted capabilities and active media.
- [ ] Support host-approved source selection, connection state, permission errors,
  session expiry, disconnect, and ICE restart/reconnection.
- [x] Provide a trusted session bridge for browser clients so the broad operator
  bearer credential is never delivered to the browser or placed in a URL.
  Browser access must be scoped to one authorized session and its capabilities.
- [ ] Define viewer keyboard mappings across Mac and Windows, including named
  keys, modifiers, key release, and focus-loss behavior. Document unsupported
  shortcuts instead of silently substituting text input.
- [ ] Evaluate separately granted pointer/button/scroll operations before calling
  the client a complete remote-desktop controller. Current keyboard transport
  alone is not full mouse-and-keyboard desktop control.

Acceptance: a viewer can attach to an explicitly approved target, display live
media, request only authorized input/audio, and immediately lose access when the
session is revoked. Closing the viewer releases its attachment without shutting
down the caller-owned desktop.

### Cross-platform and network acceptance

- [ ] Mac viewer → Mac target.
- [ ] Mac viewer → Windows target.
- [ ] Windows viewer → Mac target.
- [ ] Windows viewer → Windows target.
- [ ] Repeat each supported pairing on the same network and on different
  networks; confirm the selected ICE route in a forced TURN-relay run.
- [ ] Exercise capture, keyboard mappings, system audio, microphone, playback,
  recording, temporary-file transfer, expiry, revocation, and network recovery.
- [ ] Record target OS version, viewer/browser version, backend and codec,
  permission state, and candidate checksum for every acceptance result.

Implementation order: native audio interfaces/backends and scoped viewer access;
then a minimal video/keyboard viewer; then shared audio improvements and the
four-direction acceptance matrix. Existing Wayland/PipeWire capture and native
capture-source selection remain on the device backlog. See
[device validation](device-validation.md) for the required evidence and
[device sessions](device-sessions.md) for the current public contract.

### Cross-platform implementation increment

- [x] Add a reusable session-scoped browser WebRTC viewer and minimal UI.
- [x] Add macOS ScreenCaptureKit/microphone sources and bounded Apple playback.
- [x] Add iOS ReplayKit app capture and a broadcast-extension base with audio grant filtering.
- [x] Add Android MediaProjection, audio capture and bounded playback primitives.
- [x] Add a selected-endpoint Windows WASAPI capture/playback source library.
- [ ] Wire these native primitives into authenticated Board media sessions.
- [ ] Build the Windows library and signed mobile host integrations.
- [ ] Run all supported desktop/mobile viewer-target pairings and forced TURN.

These checked items describe source implementation only. Type-check/build results,
required host integration and remaining gaps are in [device platforms](device-platforms.md).
