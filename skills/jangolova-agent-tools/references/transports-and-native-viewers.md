# Jangolova transports and native viewers

## Server commands

```sh
# Direct agent integration through stdio MCP.
JANGOLOVA_PROVIDER_TOKEN=<injected-secret> jangolova serve-mcp

# Direct MCP over Streamable HTTP.
JANGOLOVA_PROVIDER_TOKEN=<injected-secret> \
  jangolova serve-mcp --bind 127.0.0.1:7393

# Provider HTTP API for services.
JANGOLOVA_PROVIDER_TOKEN=<injected-secret> \
  jangolova serve-engine-provider --bind 127.0.0.1:7391
```

Keep the token in the process environment or the client secret store. Do not
place it in target descriptors, helper configuration, command history,
manifests, source control, or agent-visible logs.

For a typical MCP host, the equivalent server configuration is conceptually:

```json
{
  "command": "jangolova",
  "args": ["serve-mcp"],
  "env": {"JANGOLOVA_PROVIDER_TOKEN": "provided by the host secret store"}
}
```

Exact configuration syntax belongs to the MCP host. The agent sees tools, not
the bearer token.

## MCP tools

| Tool | Use |
| --- | --- |
| `jangolova_engines` | Discover available adapters and capabilities. |
| `jangolova_instance_connect` | Attach to a caller-owned target. |
| `jangolova_instance_describe` | Read instance status and connection metadata. |
| `jangolova_instance_call` | Invoke `hello`, `capabilities`, `describe`, `act`, or `events`. |
| `jangolova_instance_events` | Read retained lifecycle and audit events. |
| `jangolova_instance_observe` | Request a configured Blockade observation; this is not an action. |
| `jangolova_action_approval_request` / `resolve` | Follow an instance's approval policy. |
| `jangolova_instance_disconnect` | Detach without stopping the target. |

Connect a native viewer using the `cymonkey` adapter and the current viewer
domain vocabulary:

```json
{
  "request": {
    "apiVersion": "interaction.engine/v1alpha1",
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

`jangolova_instance_connect` can return one-time `callerLaunch.environment`
for `macos-application` or `windows-application`. It contains the loopback
control URL, short-lived token, and exact protocol version. The agent may pass
that material to the authorized target owner through an approved secure channel.
The owner adds the absolute `JANGOLOVA_CYMONKEY_CONFIG` path and launches the
signed platform helper.

The direct `jangolova connect-engine` command is intentionally excluded from
this flow: it holds an attachment only for its own process lifetime and its
terminal output does not include `callerLaunch`. Use Provider HTTP or MCP for
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

The Provider HTTP API performs the same lifecycle:

```text
POST   /v1/instances
POST   /v1/instances/{id}/call
GET    /v1/instances/{id}/events
DELETE /v1/instances/{id}
```

Authenticate each request with `Authorization: Bearer <provider token>`.
The `call` body is `{ "method": "act", "params": { ... } }`; invoke
`capabilities` and `describe` first, then one advertised action at a time.
