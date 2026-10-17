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

Cymy writes `plugin.json` beside the executable. It declares one package
identity, one entrypoint, and the contract with every operation it serves:

```json
{
  "apiVersion": "ext.package/v1",
  "descriptor": {
    "apiVersion": "ext.plugin/v1",
    "identity": {"id": "onnx", "version": "1.0.0", "revision": "sha256:<digest>"},
    "contracts": [
      {"name": "content.provider", "version": "v1",
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
| `display.engine@v1` | Display interaction plugin |
| `content.provider@v1` | Content observation and interpretation plugin |
| `device.provider@v1` | Input and output device plugin |

The original `plugin.json` format and its `kind` field are gone. Rebuild
manifests with `cymy plugins manifest`.

## Lifecycle

```sh
cymy plugins install --manifest /path/to/plugin.json
cymy plugins list
cymy plugins upgrade --manifest /path/to/new/plugin.json
cymy plugins remove onnx
```

`--manifest` accepts `plugin.json` or its directory. Install and upgrade read
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

## Admission and verification

ext checks every call against the reviewed descriptor, and Cymonkey adds a
policy for the role of each process it opens. A policy admits operations only in
the order the protocol defines:

| Owner | Process role | Admitted |
| --- | --- | --- |
| Blockade | worker | `start` once, first; then `observe`, `interpret`, `capabilities`, `health`, `close` |
| Blockade | model inspection | `model.inspect` only |
| Board | device list | `list` only |
| Board | device session | `open` once, first; then `invoke`, `stream.open`, `stream.read`, `stream.close`, `close`. `invoke` and `stream.open` must name a capability the grant allows and, if they name a resource, one the grant covers. |
| Host (`src/internal/displayplugin`) | display probe | `inspect` only |
| Host (`src/internal/displayplugin`) | display engine session | `connect` once, first; then `call`, `authorize`, `health`, `events`, `material`, `disconnect` |

The display policy and adapter live in the host, which is the direction for
every contract: the host owns admission, and the packages only serve calls. The
Blockade and Board policies still sit in their packages until those adapters
move into the host too. These policies are defense in depth inside the plugin process. They do not
replace the host's own approvals.

Content is verified when a package is listed and again when it is launched.
Verification hashes every artifact, and a tree artifact (a vendored dependency
directory) is hashed in full, so a package with a very large tree takes seconds
to list or launch: a synthetic 625 MB, 20,000-file tree took about 6 seconds to
verify, against about 15 ms to launch a small executable. Cymonkey reuses a
verified list for 10 seconds and drops it after an install, upgrade or remove it
performs itself; a package added by another process appears when the window
ends. Keep worker processes running instead of reopening them.

Trust is "installed means trusted". Verification proves the files match the
manifest, not who published them, so install only packages you have reviewed.

## Extension boundary

A Cymonkey plugin implements one of the public contracts above and works
through Cymy. Plugin packages do not import Cymonkey's internal implementation
libraries or use their native protocol names.
