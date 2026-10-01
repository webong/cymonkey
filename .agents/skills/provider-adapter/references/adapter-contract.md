# Cymonkey content plugin contract

Read the current repository sources before implementing because the checked-in
contract is authoritative:

- `src/contracts/content.go`
- `docs/cymonkey-plugins.md`
- `docs/content.md`
- `providers/content-ultralytics/content.json`
- `providers/content-ultralytics/python/provider.py`

The last provider path is an implementation example; its directory name and
internal imports are not part of the public contract.

## Ownership

```text
agent or application
  -> Cymonkey display captures approved pixels when needed
  -> Cymonkey content selects a configured adapter
  -> content plugin calls a local model or hosted service
  -> plugin returns Cymonkey observations or findings
  -> caller decides on any later action
```

Cymonkey owns plugin installation, configuration, model and runtime storage,
process lifecycle, secret injection, request limits, public validation, and
the `cymy` commands. A plugin owns provider-specific clients, model loading,
settings, and response mapping. A display target does not configure or call
content plugins.

## Packaging

Every package has a `plugin.json` manifest with API version
`cymonkey.plugin/v1alpha1` and contract `cymonkey.content@v1alpha1`.

An executable package sets `command` and its SHA-256. A source package lists
all files and includes `content.json`:

```json
{
  "apiVersion": "cymonkey.content-source/v1alpha1",
  "runtime": {"name": "python", "version": "3.12.8"},
  "entrypoint": "python/provider.py"
}
```

Cymonkey resolves the exact installed runtime, verifies every package file,
and launches the entrypoint directly. Source packages do not ship a separate
CLI.

## Process methods

The shared handshake is `plugin.hello`. Content calls use:

- `cymonkey.content.start`
- `cymonkey.content.model.inspect`
- `cymonkey.content.observe`
- `cymonkey.content.interpret`
- `cymonkey.content.capabilities`
- `cymonkey.content.health`
- `cymonkey.content.close`

Requests and responses are newline-delimited JSON with matching IDs. Standard
error is diagnostic output. A structured error may include a stable code and
retry delay.

## Request and response behavior

Preserve a supplied request ID. Send content only to the selected endpoint or
local runtime. Preserve prompt meaning while adding only instructions needed
for structured output. Avoid recompression or resizing unless a documented
provider constraint requires it.

Observations use `cymonkey.observation-result/v1alpha1`; interpretations use
`cymonkey.content/v1alpha1`. Evidence contains concise provenance and excludes
secrets, prompts, raw responses, and chain of thought. Return a typed error when
the provider cannot produce a contract-valid result.
