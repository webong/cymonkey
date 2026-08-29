# Jangolova

Jangolova is a deployment-neutral interaction and presentation engine toolkit.
It uses Playwright, Puppeteer, Three.js, Unity, Unreal, and cooperative bridge
integrations to observe, operate, and present through caller-owned targets.

Its product goal has two equal parts: operate existing interfaces—including
clicking and typing through semantic or display-level contracts—and create
dynamic 2D/3D interfaces that agents can present and update.

Jangolova is not an agent or model gateway. Any external agent, IDE, or
deterministic application supplies planning and reasoning; Jangolova supplies
the attached, policy-governed observation and action tools through HTTP and
MCP.

Jangolova does not provision Chromium, native applications, displays,
containers, VMs, networking, or credentials. Xallet owns those concerns when
the products run together; a native user or another operator can provide the
same target endpoints and handles without Xallet.

## Included interaction engines

- **Cymonkey Master Control Plane (`jangolova.cymonkey/v1alpha2`)**: Runtime-agnostic control plane engine governing automation, interaction, and 2D/3D presentation across caller-owned targets.
  - **Automation Drivers**: Integrated Playwright and Puppeteer CDP/WebDriver BiDi drivers for browser automation primitives (`browser.navigate`, `browser.click`, `browser.fill`, `browser.press`, `browser.evaluate`, `browser.screenshot`).
  - **Web & Extension Drivers**: Jangolova WebExtension control plane, Cymonkey augmented browsing, userscripts runtime (`jangolova.cymonkey.userscript/v1alpha1`), and Safari MCP relay.
  - **Computer Domain Drivers**: Browser DOM and bounded macOS application operations via WebExtension, CDP/BiDi, Apple Events, and Accessibility.
  - **Render Domain Drivers**: Unified 2D/3D explicit-registration runtimes spanning Godot, Unity, Unreal, and Three.js.
  - **Player Domain**: Reserved for negotiated, typed content-session controls; it never transfers player lifecycle ownership to Jangolova.
- WebDriver Classic attachment to an existing caller-owned session, including Safari's `safaridriver`.
- Named WebKit WebDriver attachment for WebKitGTK, WPE WebKit, and Safari.
- Blockade pixel observation contract and external/managed YOLO/SAM vision workers.
- Target-preserving disconnect, active health, and lifecycle events.

## Commands

Discover installed interaction engines:

```bash
jangolova engines
jangolova engines --json
```

Attach directly to a browser already started by the native host or Xallet:

```bash
jangolova connect-engine \
  --adapter playwright \
  --target-kind browser \
  --endpoint cdp=http://127.0.0.1:9222
```

Cymonkey needs no extension for its CDP or WebDriver BiDi baseline. To add the
optional persistent Jangolova WebExtension backend, build it with WXT and have
the target owner install the unpacked extension from
`pkg/browser-ext/.output/<browser>-mv3`:

```bash
npm install --prefix pkg/browser-ext
npm run build:browser-extension
npm run build:macos-extension
```

Every browser artifact contains the Xallet Spook integration. It operates as an
ordinary standalone Jangolova extension when Xallet Hub is absent and activates
the Spook registration/control flow automatically when the hub is detected.

```bash
jangolova connect-engine \
  --adapter cymonkey \
  --target-kind browser \
  --endpoint cdp=http://127.0.0.1:9222 \
  --options '{"driver":"auto","extension":{"mode":"auto","id":"optional-installed-extension-id"}}'
```

`connect-engine` disconnects Jangolova when interrupted; it does not terminate
the browser.

Run the authenticated provider:

```bash
export JANGOLOVA_PROVIDER_TOKEN="replace-with-a-random-secret"
jangolova serve-engine-provider --bind 127.0.0.1:7391
```

The provider accepts caller-owned targets, creates interaction instances, and
exposes their semantic calls at `POST /v1/instances/{id}/call`.

Expose the same direct Jangolova tools to an external agent through MCP. By
default it uses stdio; pass `--bind` for Streamable HTTP:

```bash
export JANGOLOVA_PROVIDER_TOKEN="replace-with-a-random-secret"
jangolova serve-mcp
jangolova serve-mcp --bind 127.0.0.1:7393
```

Connection references, expiry, private certificate authorities, and output
redaction are documented in
[target connection security](docs/target-connection-security.md).
Credential leases renew HTTP requests and reconnect CDP/BiDi workers without
replacing the interaction instance or caller-owned runtime.
Failed adapter attachments are re-created against the same caller-owned target
without restarting that target or replaying semantic actions; see
[attachment recovery](docs/attachment-recovery.md).

## Ownership boundary

```text
Agent -> Jangolova interaction engine -> caller-owned target
             Playwright --- CDP ---------- Chromium
             Puppeteer ---- CDP/BiDi ----- Chromium/Firefox
             WebDriver ---- existing ----- WebKitGTK/WPE/Safari
             Safari MCP --- MCP relay ---- Safari 27 beta/STP
             Three.js/Unity/Unreal ------- presentation target
```

The repository boundary test prevents Chromium launch, native-process launch,
surfaces, VNC, sessions, container placement, and other target-runtime concerns
from returning to Jangolova product code. Test fixtures may create temporary
targets solely to verify attachment portability.

See [Architecture](docs/architecture.md), [Interaction provider](docs/engine-provider.md),
[Deployment modes](docs/deployment-modes.md), [Bridge protocol](docs/bridge-protocol.md),
[the headless engine test server runbook](docs/headless-engine-server.md),
[Cymonkey runtime-agnostic augmentation](docs/cymonkey-runtime.md),
[Cymonkey domains, runtimes, and drivers](docs/cymonkey-domains.md),
[Cymonkey browser integration](docs/cymonkey.md),
[browser-extension control plane](docs/browser-extension-control.md),
[Cymonkey userscripts](docs/userscripts.md),
[Jangolova macOS extension](docs/macos-extension.md),
[interface creation and operation](docs/interface-model.md),
[caller-supplied targets](docs/target-descriptor.md),
[browser target protocols](docs/browser-target-protocols.md), and
[Xallet boundary](docs/xallet-boundary.md).

## Tests

```bash
go test ./...
npm run test:browser-worker
npm run test:cymonkey
npm run test:userscripts
npm run test:macos-extension
npm run test:unity-package
npm run test:unity-cymonkey-package
npm run test:unreal-cymonkey-package
npm run test:unreal-cymonkey-fixture
npm run test:cymonkey-runtime-manifest
```

The published Unreal 5.8 headless fixture is
[`ghcr.io/webong/jangolova/unreal-cymonkey-gpu:5.8`](infra/deploy/unreal-cymonkey-gpu/README.md).
Run its live protocol check with `JANGOLOVA_CYMONKEY_TOKEN` and
`npm run test:unreal-cymonkey-live` after starting the container.

The optional container fixture is documented in
[tests/docker/README.md](tests/docker/README.md). Docker is not required by
Jangolova itself.
