# Provider packages

Cymonkey has one generic installer and process transport for optional provider
packages. Jangolova, Blockade, and Board each own their public protocol and
decide which installed packages they can use. A package can contain a reviewed
executable or reviewed source files for a runtime owned by a library.

The generic installer does not contain a list of ONNX runtimes, display
engines, device drivers, or other implementations.

## Package manifest

An executable package uses `command` and `sha256`:

```json
{
  "apiVersion": "provider.plugin/v1alpha1",
  "name": "onnx",
  "version": "1.0.0",
  "contracts": [
    {"name": "blockade.provider", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "command": "provider",
  "sha256": "<sha256 of provider>"
}
```

The fields have separate jobs:

| Field | Meaning |
| --- | --- |
| `name` | The installed package name. A library uses this to select a concrete provider, such as `onnx`. |
| `version` | The package release version. |
| `contracts` | The versioned public protocols implemented by the executable. These route the package to a library adapter. |
| `platform` | Optional `GOOS-GOARCH` restriction. |
| `command` | A filename beside `plugin.json`. It is never interpreted by a shell. |
| `sha256` | The executable digest reviewed at installation and checked again before every launch. |

A source package replaces `command` and `sha256` with a `files` array. Every
path is relative to `plugin.json`, cannot traverse or contain a symbolic link,
and is checked during install, discovery, and launch:

```json
{
  "apiVersion": "provider.plugin/v1alpha1",
  "name": "local-ultralytics",
  "version": "1.0.0",
  "contracts": [
    {"name": "blockade.provider", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "files": [
    {"path": "blockade.json", "sha256": "<sha256>"},
    {"path": "python/provider.py", "sha256": "<sha256>"}
  ]
}
```

The generic layer does not interpret `blockade.json` or Python. Blockade sees
the declared contract, reads its own descriptor, resolves its managed runtime,
and launches the entrypoint. Jangolova and Board continue to use executable
packages unless their libraries add a source runtime of their own.

Current library contracts are:

| Contract | Owner |
| --- | --- |
| `jangolova.engine@v1alpha1` | Jangolova engine protocol |
| `blockade.provider@v1alpha1` | Blockade inference provider protocol |
| `board.provider@v1alpha1` | Board device provider protocol |

These values identify protocols. A package named `onnx` declares the Blockade
contract because it speaks Blockade's provider methods. Cymonkey does not need
to know what ONNX is. The same generic inventory can retain a package for a
future contract until an adapter for that contract is present.

The original `kind` field is accepted only for existing manifests. New
manifests use `contracts`. Upgrading preserves the exact contract set. Moving a
legacy package to `contracts` therefore requires removal and a fresh install.

## Lifecycle

```sh
cymonkey plugins install --manifest /path/to/plugin.json
cymonkey plugins list
cymonkey plugins upgrade --manifest /path/to/new/plugin.json
cymonkey plugins remove onnx
```

Install and upgrade read local files, verify every declared payload, and copy
them into the user plugin directory. They do not execute the provider. Set
`CYMONKEY_PLUGIN_DIR` to override that directory.

Runtime discovery verifies the digest again. The domain edge then filters by
contract:

- Jangolova exposes matching packages as engine adapters;
- Blockade exposes matching packages by `provider: NAME` in its config;
- Board exposes matching packages by provider name in its provider commands.

## Process transport

For an executable package, the library starts the package command directly.
For a source package, the library starts its verified entrypoint with a runtime
it owns. Requests and responses are JSON
objects separated by newlines. Standard error is diagnostic output. The host
passes a minimal environment containing `PATH` and, on Windows, `SystemRoot`.
Secrets and connection material use explicit domain request fields.

The first request is:

```json
{"id":1,"method":"plugin.hello"}
```

The executable returns its manifest identity:

```json
{
  "id": 1,
  "result": {
    "apiVersion": "provider.plugin/v1alpha1",
    "name": "onnx",
    "version": "1.0.0",
    "contracts": [
      {"name": "blockade.provider", "version": "v1alpha1"}
    ],
    "platform": "darwin-arm64",
    "command": "provider",
    "sha256": "<sha256 of provider>"
  }
}
```

The API version, name, and complete contract identity must match the installed
manifest. Calls are serialized within one process and response IDs must match.
Frames are limited to 24 MiB. A canceled or timed out call terminates the
process. The handshake limit is five seconds; ordinary calls default to 60
seconds unless the caller supplies a shorter deadline.

After the handshake, the selected library owns the methods:

| Contract | Methods |
| --- | --- |
| Jangolova | `jangolova.inspect`, `jangolova.connect`, `jangolova.authorize`, `jangolova.call`, `jangolova.health`, `jangolova.events`, `jangolova.material`, `jangolova.disconnect` |
| Blockade | `blockade.start`, `blockade.model.inspect`, `blockade.observe`, `blockade.interpret`, `blockade.health`, `blockade.capabilities`, `blockade.close` |
| Board | `board.list`, `board.open`, `board.invoke`, `board.stream.open`, `board.stream.read`, `board.stream.close`, `board.close` |

The reusable manifest, install, digest, and process implementation lives in
[`lib/plugin`](../lib/plugin/). Each library's `plugin` package owns its
protocol mapping. Cymonkey connects those packages only at its executable edge;
`src/internal` does not import Jangolova, Blockade, or Board.

## Domain notes

Blockade starts a provider with one configured instance, opaque settings,
resolved secret access, and an optional verified local model. It also owns the
process count for local workers. Model inspection uses a separate short lived
process before model installation. Blockade validates every normalized result.

Board checks an explicit device and capability grant before opening a provider
session and before each operation. The executable must enforce the grant it
receives, including resource containment.

Jangolova passes public engine and target specifications. The provider handles
its own runtime and target protocol without adding implementation knowledge to
Cymonkey internals.
