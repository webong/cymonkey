# Cymonkey plugins

Cymonkey builds on the [ext plugin system](https://github.com/webong/ext/tree/main/pkg/plugin)
for optional display, content, and device plugins. ext owns the package format
(`ext.package/v1`), the verified package store, the process backend, the
handshake, and the JSON-line transport. Cymonkey supplies the domain contracts
and maps its `<contract>.<operation>` method names onto ext sessions.
Plugin authors implement a Cymonkey contract. Adapters at the composition edge
convert those requests to the internal library interfaces.

The installer verifies packages and manages their lifecycle. It does not
contain a catalog of model runtimes, display engines, or device drivers.

## Package manifest

Generate an executable manifest through Cymy:

```sh
cymy plugins manifest --type content --name onnx --version 1.0.0 \
  --command /path/to/plugin
```

Cymy writes `package.json` beside the executable. It declares one package
identity, one entrypoint, and the contract with every operation it serves:

```json
{
  "apiVersion": "ext.package/v1",
  "descriptor": {
    "apiVersion": "ext.plugin/v1",
    "identity": {"id": "onnx", "version": "1.0.0", "revision": "sha256:<digest>"},
    "contracts": [
      {"name": "cymonkey.content", "version": "v1alpha1",
       "operations": [{"name": "start"}, {"name": "observe"}, "..."]}
    ]
  },
  "artifacts": [{"name": "executable", "path": "plugin", "sha256": "<digest>", "os": "darwin", "arch": "arm64"}],
  "entrypoints": [{"name": "main", "runtime": "process", "artifact": "executable", "protocols": ["jsonline.v1"]}]
}
```

| Field | Meaning |
| --- | --- |
| `descriptor.identity` | Installed plugin ID, release version, and an immutable revision. The revision changes with any content change. |
| `descriptor.contracts` | Exact contract versions the package serves, each with its operations. |
| `artifacts` | Every file in the package with its digest. An unlisted file fails verification. |
| `entrypoints` | The executable to start. A package without entrypoints is source-only; the domain library supplies the runtime. |

Current contracts are:

| Contract | Purpose |
| --- | --- |
| `cymonkey.display@v1alpha1` | Display interaction plugin |
| `cymonkey.content@v1alpha1` | Content observation and interpretation plugin |
| `cymonkey.device@v1alpha1` | Input and output device plugin |

The original `plugin.json` format and its `kind` field are gone. Rebuild
manifests with `cymy plugins manifest`.

## Lifecycle

```sh
cymy plugins install --manifest /path/to/package.json
cymy plugins list
cymy plugins upgrade --manifest /path/to/new/package.json
cymy plugins remove onnx
```

`--manifest` accepts `package.json` or its directory. Install and upgrade read
local files, verify every declared artifact, and copy them into the Cymonkey
plugin directory. They do not execute the plugin. An upgrade must keep the same
contracts and carry a new revision, and the old install survives any failure.
Set `CYMONKEY_PLUGIN_DIR` to override that directory.

Runtime discovery verifies artifact digests again. The domain edge then selects
packages by contract:

- display plugins become display adapters;
- content plugins are selected by `plugin: NAME` in the content configuration;
- device plugins become grant-gated device adapters.

## Process transport

Cymonkey starts the entrypoint directly, without a shell, with a scrubbed
environment (`PATH`, plus `SystemRoot` on Windows, plus explicit extras).
Standard error is diagnostic output. Secrets and connection material use
explicit request fields.

The first exchange is the ext `plugin.hello` handshake. The provider answers
with its descriptor, and the host requires an exact match with the installed
package. Each call is an ext envelope carrying the plugin identity, contract,
operation, deadline, and payload. Calls are serialized within one process, and
frames are limited to 24 MiB. A cancellation or timeout terminates the process.
Providers written in Go use ext's `jsonline.ServeFunc`, and Python providers
use ext's `ext_plugin` guest SDK (vendored under
`pkg/content-ultralytics/python/ext_plugin`).

After the handshake, operations belong to the declared Cymonkey domain:

| Contract | Operations |
| --- | --- |
| Display | `inspect`, `connect`, `authorize`, `call`, `health`, `events`, `material`, `disconnect` |
| Content | `start`, `model.inspect`, `observe`, `interpret`, `health`, `capabilities`, `close` |
| Device | `list`, `open`, `invoke`, `stream.open`, `stream.read`, `stream.close`, `close` |

A structured failure retains a stable category:

```json
{
  "code": "rate_limit",
  "message": "plugin capacity is exhausted",
  "retryAfterMilliseconds": 2000
}
```

The transport preserves the code, and a failure leaves the process usable. The
Cymonkey domain adapter validates the code and maps the response into the
public API.

## Extension boundary

A Cymonkey plugin implements one of the public contracts above and works
through Cymy. Plugin packages do not import Cymonkey's internal implementation
libraries or use their native protocol names.
