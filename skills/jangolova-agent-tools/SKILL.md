---
name: jangolova-agent-tools
description: Attach and operate caller-owned Jangolova targets through MCP, HTTP, or CLI. Use when an agent needs to control a browser, native viewer, or Cymonkey render runtime; do not use to provision or launch a target runtime.
---

# Jangolova Agent Tools

Jangolova is the policy-governed tool server between an agent and a
caller-owned target. Use its advertised capabilities and returned surface IDs;
do not guess an application name, resource ID, or OS-level operation.

## Choose a transport

- **MCP stdio** is the default for an agent host that can launch local MCP
  servers. Configure its Jangolova server command as `jangolova serve-mcp` and
  provide `JANGOLOVA_PROVIDER_TOKEN` through the host's secret environment.
  Use the discovered `jangolova_*` MCP tools; do not manually write JSON-RPC to
  the server's standard input.
- **Engine Provider HTTP** is for an external service, daemon, or remote
  integration. Run `jangolova serve-engine-provider --bind <host:port>` and
  call it with the provider bearer token.
- **MCP over HTTP** is for an MCP client that cannot use stdio. Run
  `jangolova serve-mcp --bind <host:port>` and use its `/mcp` Streamable HTTP
  endpoint.
- **`connect-engine`** is a terminal diagnostic tool for targets that need no
  caller-launch handoff. It disconnects when the command exits and does not
  return native-helper launch material, so do not use it for macOS/Windows
  cooperative viewers or as a long-lived agent integration.

Read [the transport and native-viewer playbook](references/transports-and-native-viewers.md)
when connecting a target or handing off a macOS/Windows helper.

## Agent control sequence

1. Call `jangolova_engines` and select only an available adapter.
2. Call `jangolova_instance_connect` with a caller-owned target descriptor.
3. For a native macOS/Windows target, return `callerLaunch.environment` only
   to the authorized target owner. The owner adds its helper configuration
   path and launches its signed helper; the agent must not launch it.
4. Call `jangolova_instance_call` with `hello`, `capabilities`, and
   `describe`.
5. Use only capabilities advertised in the current response and surface IDs
   returned by `describe`.
6. After a mutation, read `jangolova_instance_events`; request an approval
   only when the capability/instance requires it.
7. Call `jangolova_instance_disconnect` when the task is done. This detaches
   Jangolova and must not terminate the target.

## Native viewer boundary

For `macos-application` and `windows-application`, the target owner—not the
agent—chooses allowed bundle IDs or executable names in the helper
configuration. An agent works with a resulting `surfaceId`, for example:

```json
{
  "method": "act",
  "params": {
    "name": "display.capture",
    "input": {"surfaceId": "windows-viewer:4812-924182"}
  }
}
```

Never request raw AppleScript, raw Apple Events, PowerShell, process launch,
raw Win32 messages, a whole accessibility tree, or desktop input outside an
advertised viewer surface.
