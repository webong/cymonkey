# Cymonkey display service

Cymonkey implements the authenticated `cymonkey.display/v1alpha1` API. A target
provider supplies connection details for a caller owned runtime; it does not
become a Cymonkey dependency.

An external agent or application plans and chooses calls. The display service
attaches targets, enforces policy and approvals, executes actions, and retains
audit events.

The caller may set `engine.adapter` to `auto` and supply a formal
`cymonkey.display-target/v1alpha1` descriptor. Cymonkey selects by protocol and
required capabilities. See
[Caller-supplied interaction targets](target-descriptor.md).
Opaque credential and TLS references use the secret-safe
[target connection security layer](target-connection-security.md).

Unity and Unreal semantic presentation targets use the provider-visible
`cymonkey` adapter with `domain: "render"` and a caller-owned
`websocket` endpoint. The adapter attaches to the application; it does not
own its renderer or lifecycle.

```bash
export CYMONKEY_DISPLAY_TOKEN="a-random-session-secret"
cymy display serve --bind 127.0.0.1:7391
```

## Operations

- `GET /healthz`
- `GET /v1/display-adapters`
- `POST /v1/display-instances`
- `GET /v1/display-instances/{instanceId}`
- `POST /v1/display-instances/{instanceId}/call`
- `POST /v1/display-instances/{instanceId}/approvals`
- `POST /v1/display-instances/{instanceId}/approvals/{approvalId}`
- `GET /v1/display-instances/{instanceId}/events`
- `POST /v1/display-reconcile`
- `DELETE /v1/display-instances/{instanceId}`

### Visual capture boundary

The display service exposes semantic interaction calls, including a negotiated
`window.screenshot` where supported. Cymonkey may send the captured pixels to
the content service. A later display action is a separate policy and approval
governed request.

## Direct MCP tools

`cymy display serve-tools` exposes the same display service operations as direct
MCP tools over stdio, or Streamable HTTP with `--bind`. It has no model
connector, agent session, prompt endpoint, or internal planning loop.

The MCP tool names are `cymonkey_display_adapters`,
`cymonkey_display_connect`, `cymonkey_display_describe`,
`cymonkey_display_call`, `cymonkey_display_events`,
`cymonkey_display_approval_request`, `cymonkey_display_approval_resolve`, and
`cymonkey_display_disconnect`. MCP uses the same display token and policy gates
as HTTP.

## Approval receipts and audit events

An instance can declare `engine.approval.requiredActions` when it is
connected. Before calling one of those semantic actions, an external agent
requests a pending approval with the exact `act` parameters, an owner-facing
client resolves it, and the agent supplies the one-time `approvalId` on the
action call. Approval receipts are bound to the action and parameters, expire
quickly, and cannot be reused.

Every action contributes retained instance events: `action.requested`,
`action.denied`, `action.failed`, `action.completed`, and—where
configured—approval requested, approved/rejected, and consumed events. This is
execution policy and evidence, not agent reasoning.

### Declarative reconcile (`POST /v1/display-reconcile`)

The reconcile endpoint accepts a **desired-state list** of interaction instances.
Cymonkey display service creates missing instances, retains existing matching ones, and —
when `prune: true` — disconnects and removes instances not in the desired set.
This lets callers (such as Xallet or an orchestration operator) reconcile the
current engine state against their own manifest without per-instance calls.

```json
{
  "apiVersion": "cymonkey.display/v1alpha1",
  "prune": false,
  "desired": [
    {
      "apiVersion": "cymonkey.display/v1alpha1",
      "instanceId": "browser-one",
      "engine": { "adapter": "playwright" },
      "target": { "kind": "browser" }
    },
    {
      "apiVersion": "cymonkey.display/v1alpha1",
      "instanceId": "browser-two",
      "engine": { "adapter": "playwright" },
      "target": { "kind": "browser" }
    }
  ]
}
```

Each entry in `desired` follows the same shape as a `POST /v1/display-instances` request.
Duplicated instance IDs are reported as failures rather than double-connected.

Response:

```json
{
  "apiVersion": "cymonkey.display/v1alpha1",
  "reconciled": 2,
  "created": ["browser-two"],
  "retained": ["browser-one"],
  "failed": {}
}
```

- **`reconciled`** — count of instances in the engine after the operation.
- **`created`** — instance IDs that were newly connected.
- **`retained`** — instance IDs that already existed and were kept.
- **`pruned`** — instance IDs that were disconnected and removed (only present
  when `prune: true`).
- **`failed`** — map of instance ID to error message for entries that could not
  be connected, including validation errors, unknown adapters, adapter-selection
  failures, or target-resolution errors.

When `prune` is `true`, Cymonkey display service cancels any in-progress recovery for pruned
instances, waits for recovery to settle, disconnects the display adapter,
and releases credential material — all before returning the response.

Connect explicitly to Chromium that the caller already owns:

```json
{
  "apiVersion": "cymonkey.display/v1alpha1",
  "instanceId": "browser-one",
  "engine": {
    "adapter": "playwright"
  },
  "target": {
    "kind": "browser",
    "endpoints": [
      {
        "name": "cdp",
        "protocol": "cdp",
        "url": "http://127.0.0.1:9222"
      }
    ]
  }
}
```

The legacy compact target shape above remains accepted. New integrations
should include `target.apiVersion` and `target.targetId`. Endpoint descriptors
may also carry opaque `credentialRef`, `tlsRef`, audience, and string metadata
without embedding secrets.

Puppeteer can instead attach to a caller-owned WebDriver BiDi endpoint:

```json
{
  "apiVersion": "cymonkey.display/v1alpha1",
  "instanceId": "firefox-one",
  "engine": {"adapter": "puppeteer"},
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "webdriver-bidi",
      "protocol": "webdriver-bidi",
      "url": "ws://127.0.0.1:9223/session"
    }]
  }
}
```

Attach Cymonkey display service's declarative web presentation bridge to a caller-owned
Chromium page. The target provider is responsible for starting Chromium,
serving the presentation URL, and supplying the CDP endpoint:

```json
{
  "apiVersion": "cymonkey.display/v1alpha1",
  "instanceId": "presentation-one",
  "engine": {
    "adapter": "web-presentation",
    "source": "http://127.0.0.1:8080/etc/web-presentation/",
    "options": {
      "policy": {
        "allowedSourceOrigins": ["http://127.0.0.1:8080"],
        "allowedAssetOrigins": ["self"],
        "authorizedActions": ["presentation.capture", "presentation.execute", "presentation.mount"],
        "executeTimeoutMillis": 5000,
        "captureTimeoutMillis": 10000,
        "mountTimeoutMillis": 15000
      }
    }
  },
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "cdp",
      "protocol": "cdp",
      "url": "http://127.0.0.1:9222"
    }]
  }
}
```

The presentation adapter does not launch Chromium, allocate a display, or
serve files. Those responsibilities remain with the caller's target provider,
which may be Xallet, a native host, or a direct-container supervisor.
Once connected, call `act` with `presentation.create` or
`presentation.replace` for structured documents, or `presentation.write` with
HTML/CSS/JavaScript source for a complete authored surface. Then use
`presentation.patch`, `presentation.execute`, and `presentation.activate` for
incremental updates. `presentation.capture` returns a PNG captured by the
attached browser. Document mutations carry `expectedStateRevision`, while
`presentation.mount` loads a versioned artifact from an engine-supported
caller-supplied location. See
[Web presentation provider handoff](presentation-provider.md) for artifact
limits, origin policy, sensitive-action authorization, audit events, timeouts,
and conflict behavior.

WebDriver Classic attaches to an existing caller-owned driver session. The
target provider creates and later deletes that session:

```json
{
  "apiVersion": "cymonkey.display/v1alpha1",
  "instanceId": "safari-one",
  "engine": {"adapter": "webdriver-classic"},
  "target": {
    "kind": "browser",
    "endpoints": [{
      "name": "webdriver",
      "protocol": "webdriver",
      "url": "http://127.0.0.1:4444"
    }],
    "handles": {"webdriver.sessionId": "caller-created-session"}
  }
}
```

Use `webkit-webdriver` with the same target shape for WebKitGTK, WPE WebKit,
or a WebKit-specific Safari attachment. For Safari MCP, select `safari-mcp` and
provide an endpoint with protocol `mcp-streamable-http`; Cymonkey display service discovers
the relay's MCP tools and exposes their input schemas as interaction
capabilities.

The response describes the interaction instance, not the target:

```json
{
  "apiVersion": "cymonkey.display/v1alpha1",
  "instanceId": "browser-one",
  "adapter": "playwright",
  "status": "connected",
  "health": {
    "status": "connected",
    "observedAt": "2026-08-01T12:00:00Z"
  },
  "capabilities": ["window.click", "window.evaluate", "window.fill"]
}
```

Cooperative native helpers are the exception that may add `callerLaunch` to
the successful `POST /v1/display-instances` response. For example, connecting the
Cymonkey `viewer` / `macos-app` attachment returns an ephemeral loopback control URL, bearer
token, and exact protocol as launch environment. The authorized target owner
adds its configuration path and launches its signed helper:

```json
{
  "callerLaunch": {
    "environment": {
      "CYMONKEY_CONTROL_URL": "ws://127.0.0.1:49152/bridge",
      "CYMONKEY_CONTROL_TOKEN": "ephemeral-secret",
      "CYMONKEY_PROTOCOL": "cymonkey/v1alpha1"
    }
  }
}
```

This material appears only in the initial authenticated connection response.
It is not retained in subsequent instance descriptions, events, or logs.
Cymonkey display service creates the control attachment but never launches or signs the
caller-owned helper.

`CYMONKEY_CONTROL_TOKEN` authenticates this helper's outbound connection.
Render packages that host their own socket use `CYMONKEY_TOKEN` instead.
`CYMONKEY_DISPLAY_TOKEN` authenticates the display service; these credentials
are not interchangeable.

Call the common semantic protocol:

```http
POST /v1/display-instances/browser-one/call
Authorization: Bearer ...
Content-Type: application/json

{
  "method": "act",
  "params": {
    "name": "window.fill",
    "input": {"selector": "#email", "value": "agent@example.com"}
  }
}
```

Supported browser actions currently include navigation, click, fill, press,
JavaScript evaluation, and screenshots. Capability metadata reports effect and
input schema. Before dispatching an `act` call, Cymonkey display service invokes each
adapter's **per-capability policy gate** via the `Authorize` method on the
connected instance. If the action is rejected the response is a **403
`policy_denied`** with the adapter's reason. This gate applies only to the
`act` method; other methods pass through without authorization.

Deleting an instance disconnects Cymonkey display service. It must not terminate the browser
or native target. Lifecycle events are bounded to 256 entries and addressed by
cursor through the `/events` operation.

See [Browser target protocols and Xallet handoff](browser-target-protocols.md)
for protocol selection, address translation, security, and cleanup ownership.
