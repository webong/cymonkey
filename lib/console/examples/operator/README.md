# Explicit Console host configuration

This example is configuration for `cymy console`, not a live model fixture.
Replace the `.invalid` API root and placeholder model ID, and set only the
credential variable you name in the configuration. No endpoint is contacted
merely by listing this configuration's local models/tools.

```sh
cymy console models --config /absolute/path/config.json
cymy console tools --config /absolute/path/config.json
```

To request one model turn, provide a JSON file such as:

```json
{
  "model": "selected-text-model",
  "messages": [{"role": "user", "parts": [{"kind": "text", "text": "Explain this task."}]}]
}
```

Then explicitly run `cymy console generate --config ... --input ...`. Any tool
proposal is returned as data. Human/agent callers select individual invocations;
the host never follows proposals automatically.

See [Console configuration](../../../../docs/console.md) for borrowed display,
device, MCP and speech bindings. This example has not been run against a live
model endpoint.
