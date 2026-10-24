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

## Installing the packages

The host links none of its guests; each runs as an installed package. One
script builds every guest in its own module and installs it:

```sh
scripts/install-packages.sh                      # display device content console gateway mobile
scripts/install-packages.sh gateway mobile       # only some roles
scripts/install-packages.sh providers            # the ONNX and WebLLM content providers
cymy plugins check                               # what is installed, what is missing
```

Re-running upgrades a package whose content changed and leaves the rest
(`unchanged`). It installs the display engine once for each of `playwright`,
`puppeteer` and `cymonkey`. `cymy plugins check` lists each role, the packages
serving it and the commands that fail without it. The sections below show the
individual build, manifest and install commands the script runs.

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
| `content.service@v1` | The content engine itself: runs `cymy content` commands and hosts the content providers |
| `device.provider@v1` | Input and output device plugin |
| `mobile.target@v1` | One mobile target (app media bridge): `start`, `stop` and the viewer's device operations, plus `events` |

The original `plugin.json` format and its `kind` field are gone. Rebuild
manifests with `cymy plugins manifest`.

## Display engine package

Board's display engine runs as an ext package that serves `display.engine`. It
replaces the engine that used to be linked into `cymy`: the host no longer
registers `playwright`, `puppeteer` or `cymonkey` itself.

The package is installed once per engine name it serves, and the package name is
the engine name. `playwright` and `puppeteer` pin the backend to that driver;
any other name uses the generic engine:

```sh
go -C pkg/board build -o "$PWD/display" ./cmd/display
for engine in playwright puppeteer cymonkey; do
  cymy plugins manifest --type display --name $engine --version 1.0.0 --command ./display
  cymy plugins install --manifest ./plugin.json
done
```

`cymy plugins manifest --type display` declares the `display.host` reverse
contract in the package, so the engine can ask the host while a session is
connected. The host owns endpoint policy and credentials, so the engine asks for
three things instead of carrying its own copies: `validate_endpoint`,
`worker_environment` (the environment for a browser worker that will connect to
an authenticated endpoint) and `redact` (removing the target's secrets from a
message). The host answers from its own copy of the target, and a request can
name an endpoint but never supply its credentials. The engine keeps the
connection headers it was sent so it can reach the target, and the redaction
list never leaves the host.

The package serves cooperative targets (macOS and Windows helpers, which connect
back to the engine with the launch variables it returns) and browser and CDP
targets through a Node worker; the host passes it the worker script path when it
launches it. The Safari bridge is a separate host engine, `safari-mcp`, and is
not offered through this package.

## Console service package

`cymy console` runs in the console, which is an ext package that serves
`console.service`. Like the content service it is a command service: the host
forwards the command, its stdout comes back whole, and its log lines stream as
progress. Build and install it once:

```sh
go -C pkg/console build -o "$PWD/console" ./cmd/console
cymy plugins manifest --type console-service --name console --version 1.0.0 --command ./console
cymy plugins install --manifest ./plugin.json
```

The package starts with a scrubbed environment, so the host passes only the
variables the console configuration refers to by name (the operator or MCP token,
a model API key) and a command's `--token-env`. A command runs in the directory
it was started from, so relative paths in its arguments mean what you typed.
`call` and `generate` read their request from standard input; the host reads it
(up to 1 MiB) and sends it with the call. The commands that speak a stdio
protocol of their own, `mcp` and `stream`, cannot be a single call, so the host
runs the package's verified executable as a child with the same restricted
environment and your terminal's input and output.

## Display gateway package

`cymy device gateway serve` runs in the display gateway, an ext package that
serves `gateway.service` and relays a viewer's WebSocket to one private display
endpoint. It is a command service like the content and console services, and a
long call: its "listening" line streams back, and an interrupt stops it.

```sh
go -C pkg/board build -o "$PWD/gateway" ./cmd/gateway
cymy plugins manifest --type gateway-service --name gateway --version 1.0.0 --command ./gateway
cymy plugins install --manifest ./plugin.json
```

The package is given only the token variable named by `--token-env` (default
`CYMONKEY_DEVICE_GATEWAY_TOKEN`) and the defaults the command reads
(`CYMONKEY_DEVICE_GATEWAY_BIND`, `CYMONKEY_DISPLAY_UPSTREAM`).

## Mobile target package

`POST /v1/device-targets` starts one process per target from a `mobile-target`
package. Install it once (see [mobile target sessions](device-mobile-sessions.md)):

```sh
go -C pkg/board build -o "$PWD/mobile" ./cmd/mobile
cymy plugins manifest --type mobile-target --name mobile --version 1.0.0 --command ./mobile
cymy plugins install --manifest ./plugin.json
```

The package declares a loopback `ws` endpoint for the phone's frames; the host
admits it with `endpoint.Admit` and relays the phone's socket to it.

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
| Device | `list`, `open`, `invoke`, `stream.open`, `stream.read`, `stream.close`, `close`, and optional `events` (a session's lifecycle events; the host polls it only for packages that declare it) |
| Mobile target | `start`, `stop`, and the device operations above |

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
| Host (`mobilekit`) | mobile target | `start` once, first; `stop` once; between them the viewer operations, where `open` may repeat after `close` and `invoke` and `stream.open` follow the latest grant |
| Host (`src/internal/displayplugin`) | display probe | `inspect` only |
| Host (`src/internal/displayplugin`) | display engine session | `connect` once, first; then `call`, `authorize`, `health`, `events`, `material`, `disconnect` |

The display policy and adapter live in the host, which is the direction for
every contract: the host owns admission, and the packages only serve calls. The
Blockade and Board policies still sit in their packages until those adapters
move into the host too. These policies are defense in depth inside the plugin process. They do not
replace the host's own approvals.

Listing reads each package's manifest only, so discovery costs the same for a
small executable and a package with a huge vendored tree. Content is verified
when a plugin is opened: ext hashes every artifact, and a tree artifact in
full, before it starts the process. That costs seconds for a very large tree (a
synthetic 625 MB, 20,000-file tree took about 6 seconds, against about 15 ms for
a small executable), so keep worker processes running instead of reopening them.
`cymy plugins list` also reports the packages it could not read under
`unreadable`; a package whose content changed after install is listed and fails
when it is opened.

Trust is "installed means trusted". Verification proves the files match the
manifest, not who published them, so install only packages you have reviewed.

## Extension boundary

A Cymonkey plugin implements one of the public contracts above and works
through Cymy. Plugin packages do not import Cymonkey's internal implementation
libraries or use their native protocol names.
