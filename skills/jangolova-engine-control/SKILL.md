---
name: jangolova-engine-control
description: Connect Jangolova to an authenticated Pacman WebSocket and control explicitly registered engine resources using hello, capabilities, describe, act, events, and health. Use when an agent needs to inspect or manipulate a running Godot, Unity, or Unreal runtime.
---

# Jangolova Engine Control

Use this skill only after the endpoint and bearer token are supplied through protected configuration. Never print or persist the token.

## Attach safely

1. Connect to the configured `ws://` or `wss://` endpoint.
2. Authenticate immediately using the runtime's documented handshake.
3. Call `hello`, `capabilities`, `describe`, and `health`.
4. Treat `describe` as authoritative for resource IDs and `capabilities` as authoritative for action names and input shapes.
5. Invoke only actions advertised and explicitly registered for the target.
6. Poll `events` with a cursor after mutations and close the socket cleanly.

The protocol version is `jangolova.cymonkey/v1alpha2` (engines advertise
`jangolova.pacman/v1alpha1` as a compatible legacy protocol). After
authentication, requests use `{id, method, params}`:

```json
{"id":1,"method":"act","params":{"name":"object.visibility.set","input":{"targetId":"object:fixture","visible":false}}}
```

Godot's fixture accepts an authentication frame:

```json
{"type":"auth","token":"<runtime-token>"}
```

Unreal authenticates the already-upgraded WebSocket with an HTTP `Authorization: Bearer <runtime-token>` header through its listener binding.

## Control rules

- Do not guess target IDs or action names.
- On `action_not_allowlisted`, stop and report the missing registration rather than retrying with a guessed ID.
- Expect action results and events to be asynchronous; use `events` instead of assuming local state changed.
- Never use Pacman to terminate the engine, destroy its world, or bypass its lifecycle boundary.

## Baseline sequence

```text
hello → capabilities → describe → health
act (one advertised action) → events(after cursor) → describe
```

Read [references/protocol-playbook.md](references/protocol-playbook.md) for message examples and executable repository test references.
