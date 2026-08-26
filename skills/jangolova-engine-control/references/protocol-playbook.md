# Cymonkey engine-profile protocol playbook

Shared methods:

```text
hello, capabilities, describe, act, events, health
```

After authentication, requests use `{id, method, params}` and replies correlate by `id`:

```json
{"id":1,"method":"hello","params":{}}
{"id":2,"method":"capabilities","params":{}}
{"id":3,"method":"describe","params":{}}
{"id":4,"method":"health","params":{}}
```

An action request uses `name` and an engine-defined `input` object carrying
`targetId` and an optional `expectedRevision` guard:

```json
{"id":5,"method":"act","params":{"name":"object.visibility.set","input":{"targetId":"object:fixture","visible":true}}}
```

A stale or missing revision guard on mutating actions returns a
`stale_revision` error once the engine exposes revisions.

For event polling, retain the returned cursor and pass it as `after`:

```json
{"id":6,"method":"events","params":{"after":"0","limit":50}}
```

Use these repository tests as executable examples:

- `tests/godot-cymonkey-house-live-test.mjs`
- `tests/unreal-cymonkey-fixture-contract-test.mjs`
- `tests/unity-cymonkey-fixture-contract-test.mjs`

The engine owner must register every resource and action explicitly; these tests do not authorize discovery.
