# Next-chat handoff: Jangolova presentation work

This document is the restart point for the next implementation chat.

## Current state

The repository now has a provider-visible `web-presentation` adapter. It
attaches to a browser that somebody else already started through a CDP
endpoint. The adapter launches only a short-lived Node/Puppeteer interaction
worker; it does not launch, stop, provision, or display a browser.

The reference page is [examples/web-presentation](../examples/web-presentation).
It exposes `window.cymonkey.jangolova` and can render either structured documents or
authored HTML/CSS/JavaScript.

Implemented operations:

- `presentation.create` and `presentation.replace` for structured documents or
  HTML-source documents;
- `presentation.write` for `{html, css, js}` source;
- `presentation.mount` for provider-neutral, versioned artifact references;
- `presentation.execute` for incremental JavaScript;
- `presentation.patch` with bounded `set`, `remove`, and `append` operations;
- `presentation.describe`, `presentation.activate`, and `presentation.capture`;
- cursor-based `events` for presentation actions and resize events.

The adapter is registered as `web-presentation` and is discoverable through
`cymonkey provider engines --json`. The provider handoff is documented in
[presentation-provider.md](presentation-provider.md).

The engine provider also accepts the formal
`interaction.target/v1alpha1` descriptor and `engine.adapter: "auto"`.
Automatic selection uses endpoint protocols and required capabilities only;
native, container, VM, remote, and Xallet-owned targets share the same path.
See [target-descriptor.md](target-descriptor.md).

Opaque credential and TLS references are now resolved immediately before
adapter connection through the deployment-neutral layer documented in
[target-connection-security.md](target-connection-security.md). Resolved
headers never enter provider payloads or command arguments, are redacted from
outbound errors/events/health, and are released on disconnect. The direct
container fixture puts Chromium behind an authenticated CDP relay and proves
the complete reference-resolution path. Credential leases now re-resolve
before expiry: HTTP adapters consume the current generation per request, while
CDP/BiDi workers reconnect in place and emit
`interaction.connection.renewed`. Live CA/client-certificate rotation remains
separate transport work.

Validation already run:

```bash
GOCACHE=/tmp/jangolova-go-build-cache go test -race ./...
npm run test:browser-worker
npm run test:presentation-worker
docker compose -f tests/docker/compose.yaml run --rm \
  --entrypoint tests/docker/web-presentation-smoke-test.sh engine-test
```

The live Chromium smoke test writes authored HTML/CSS/JavaScript, verifies the
resulting DOM and cursor event, validates a captured PNG, disconnects the
provider, and proves that the caller-owned Chromium and presentation server
remain alive. Connected capability discovery also retains the provider's
common `describe`, `act`, and `events` methods alongside page-declared actions.
It also verifies source and asset-origin policy, bounded inline artifacts, and
optimistic revisions by rejecting a stale write without changing the surface.
The same test is a direct-container conformance fixture: its supervisor owns
Xvfb, Chromium, two localhost artifact origins, and Jangolova. It mounts an
artifact between origins, separates immutable `artifactRevision` from live
`stateRevision`, and requires no Xallet API or identifier.
Sensitive presentation actions are now policy-gated through
`authorizedActions`, bounded by `executeTimeoutMillis` and
`captureTimeoutMillis`/`mountTimeoutMillis`, and audited through provider instance events such as
`presentation.execute.requested`, `.succeeded`, `.denied`, `.failed`, and
`.cancelled`.

## Important boundary

Jangolova owns presentation definitions, generated source, semantic actions,
events, the provider-neutral artifact contract, and engine integrations. The
target provider—Xallet, a native host, or a direct-container supervisor—owns:

- serving the HTML/JS assets or presentation URL;
- Chromium, Firefox, WebKit, Unity, Unreal, or other runtime processes;
- CDP/WebDriver/MCP endpoints and address translation;
- windows, surfaces, display placement, GPU, VNC/WebRTC, and input injection;
- credentials, network policy, container/VM placement, and cleanup.

The adapter must remain target-preserving: disconnecting Jangolova cannot stop
the caller-owned runtime.

## What is still missing

### 1. Harden JavaScript execution

`presentation.execute` intentionally runs code inside the target page and is
marked externally effectful. The adapter now enforces provider authorization,
emits audit events, and bounds execution time. Remaining work is to define a
clearer trusted-presentation-code versus untrusted-agent-code model so callers
do not silently treat arbitrary JavaScript as a safe semantic action.

### 2. Improve document semantics

The reference renderer is deliberately small. Define a stable document schema
for layout, text, media, controls, and accessible names. Add validation and
revision-aware patching so concurrent agents cannot overwrite each other's
changes. Add richer `describe` output containing semantic nodes and action
schemas.

### 3. Connect Three.js as a first-class presentation host

`examples/threejs-scene` already exposes scene actions and pointer events, but
it is not converted to the new `presentation.*` document contract. Decide
whether Three.js remains an engine-specific bridge or also implements the
common presentation artifact lifecycle. Add an adapter/fixture if common
operations are required.

### 4. Add Unity and Unreal provider attachment

Unity has a native bridge package, but provider-level attachment is still
unchecked in the roadmap. Define the endpoint/handle shape Xallet will pass,
add an adapter conformance test, and implement the Unreal plugin against the
same hello/capabilities/describe/act/events contract.

### 5. Build the display-level fallback

The current browser adapters are semantic/runtime adapters. The planned
provider-neutral display adapter still needs `display.describe`,
`display.capture`, pointer, keyboard, focus, coordinate-space, and input-policy
operations. Xallet should own the VNC/WebRTC/OS mechanism; Jangolova should
translate approved agent intent into that contract.

### 6. Harden operations and packaging

Remaining production work includes per-capability authorization, audit logs,
reconnection/orphan recovery, generated protocol clients, compatibility
fixtures, and versioned runtime packaging. The un-published image name should
remain `jangolova/engine-runtime` until a release process exists.

## Recommended next build order

1. Define the trusted versus untrusted presentation-code model.
2. Normalize the document schema and semantic `describe` response.
3. Decide and implement the Three.js common-host path.
4. Continue with Unity/Unreal attachment, then display-level input.

## Useful files

- Adapter: `src/adapters/webpresentation/adapter.go`
- Worker: `scripts/presentation-worker.mjs`
- Reference host: `examples/web-presentation/main.js`
- Provider API docs: `docs/engine-provider.md`
- Xallet contract: `docs/presentation-provider.md`
- Product model: `docs/interface-model.md`
- Broad checklist: `docs/roadmap.md`
