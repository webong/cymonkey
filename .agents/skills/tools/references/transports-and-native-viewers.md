# Cymonkey display transports and native viewers

## Server commands

```sh
# Direct agent integration through stdio MCP.
CYMONKEY_DISPLAY_TOKEN=<injected-secret> cymy display serve-tools

# Direct MCP over Streamable HTTP.
CYMONKEY_DISPLAY_TOKEN=<injected-secret> \
  cymy display serve-tools --bind 127.0.0.1:7393

# Display HTTP API for services.
CYMONKEY_DISPLAY_TOKEN=<injected-secret> \
  cymy display serve --bind 127.0.0.1:7391
```

Keep the token in the process environment or the client secret store. Do not
place it in target descriptors, helper configuration, command history,
manifests, source control, or agent-visible logs.

For a typical MCP host, the equivalent server configuration is conceptually:

```json
{
  "command": "cymy",
  "args": ["display", "serve-tools"],
  "env": {"CYMONKEY_DISPLAY_TOKEN": "provided by the host secret store"}
}
```

Exact configuration syntax belongs to the MCP host. The agent sees tools, not
the bearer token.

## MCP tools

| Tool | Use |
| --- | --- |
| `cymonkey_display_adapters` | Discover available adapters and capabilities. |
| `cymonkey_display_connect` | Attach to a caller-owned target. |
| `cymonkey_display_describe` | Read instance status and connection metadata. |
| `cymonkey_display_call` | Invoke `hello`, `capabilities`, `describe`, `act`, or `events`. |
| `cymonkey_display_events` | Read retained lifecycle and audit events. |
| `cymonkey_display_approval_request` / `cymonkey_display_approval_resolve` | Follow an instance's approval policy. |
| `cymonkey_display_disconnect` | Detach without stopping the target. |

Connect a native viewer using the `cymonkey` adapter and the current viewer
domain vocabulary:

```json
{
  "request": {
    "apiVersion": "cymonkey.display/v1alpha1",
    "instanceId": "native-viewer-one",
    "engine": {
      "adapter": "cymonkey",
      "options": {"domain": "viewer", "driver": "auto"}
    },
    "target": {"kind": "windows-application"}
  }
}
```

For a browser target, add its caller-owned CDP, BiDi, or other supported
endpoint to `target.endpoints`. For a render runtime, choose `domain: "render"`
and supply its caller-owned authenticated `websocket` endpoint.

## Native helper handoff

`cymonkey_display_connect` can return one-time `callerLaunch.environment`
for `macos-application` or `windows-application`. It contains the loopback
control URL, short-lived token, and exact protocol version. The agent may pass
that material to the authorized target owner through an approved secure channel.
The owner adds the absolute `CYMONKEY_CONFIG` path and launches the
signed platform helper.

The direct `cymy display connect` command is intentionally excluded from
this flow: it holds an attachment only for its own process lifetime and its
terminal output does not include `callerLaunch`. Use display HTTP or MCP for
native helpers.

The helper's configuration determines the actual native scope:

- macOS: `allowedBundleIds`, Apple Event mappings, Accessibility policy, and
  optional viewer policy.
- Windows: `allowedExecutableNames` and optional viewer policy.

Until the helper connects, the attachment is waiting. Do not retry arbitrary
actions; wait for owner confirmation, then call `hello`, `capabilities`, and
`describe`. Missing operating-system consent reduces the advertised capability
set. It is not a reason to attempt a fallback script or raw native API.

## HTTP equivalent

The display HTTP API performs the same lifecycle:

```text
POST   /v1/display-instances
POST   /v1/display-instances/{id}/call
GET    /v1/display-instances/{id}/events
DELETE /v1/display-instances/{id}
```

Authenticate each request with `Authorization: Bearer <display token>`.
The `call` body is `{ "method": "act", "params": { ... } }`; invoke
`capabilities` and `describe` first, then one advertised action at a time.
