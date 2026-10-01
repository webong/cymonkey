# Cymonkey plugins

Cymonkey has one installer and process transport for optional display, content,
and device plugins. Plugin authors implement a Cymonkey contract. Adapters at
the composition edge convert those requests to the internal library interfaces.

The installer verifies packages and manages their lifecycle. It does not
contain a catalog of model runtimes, display engines, or device drivers.

## Package manifest

Generate an executable manifest through Cymy:

```sh
cymy plugins manifest --type content --name onnx --version 1.0.0 \
  --command /path/to/plugin
```

The resulting `plugin.json` uses the generic package schema and a Cymonkey
contract:

```json
{
  "apiVersion": "cymonkey.plugin/v1alpha1",
  "name": "onnx",
  "version": "1.0.0",
  "contracts": [
    {"name": "cymonkey.content", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "command": "plugin",
  "sha256": "<sha256 of plugin>"
}
```

| Field | Meaning |
| --- | --- |
| `name` | Installed plugin name, such as `onnx`. |
| `version` | Package release version. |
| `contracts` | Versioned Cymonkey protocols implemented by the package. |
| `platform` | Optional `GOOS-GOARCH` restriction. |
| `command` | Executable filename beside `plugin.json`; never interpreted by a shell. |
| `sha256` | Digest checked during installation and before every launch. |

Current contracts are:

| Contract | Purpose |
| --- | --- |
| `cymonkey.display@v1alpha1` | Display interaction plugin |
| `cymonkey.content@v1alpha1` | Content observation and interpretation plugin |
| `cymonkey.device@v1alpha1` | Input and output device plugin |

The original `kind` field is read only for legacy manifests. New manifests use
`contracts`.

## Lifecycle

```sh
cymy plugins install --manifest /path/to/plugin.json
cymy plugins list
cymy plugins upgrade --manifest /path/to/new/plugin.json
cymy plugins remove onnx
```

Install and upgrade read local files, verify every declared payload, and copy
them into the Cymonkey plugin directory. They do not execute the plugin. Set
`CYMONKEY_PLUGIN_DIR` to override that directory.

Runtime discovery verifies payload digests again. The domain edge then selects
packages by contract:

- display plugins become display adapters;
- content plugins are selected by `plugin: NAME` in the content configuration;
- device plugins become grant-gated device adapters.

## Process transport

Requests and responses are newline-delimited JSON. Standard error is diagnostic
output. Cymonkey starts commands directly, without a shell, and passes a small
environment. Secrets and connection material use explicit request fields.

The first request is always:

```json
{"id":1,"method":"plugin.hello"}
```

The result must exactly identify the installed manifest. Calls are serialized
within one process, response IDs must match, and frames are limited to 24 MiB.
A cancellation or timeout terminates the process.

After the handshake, methods use the declared Cymonkey domain:

| Contract | Methods |
| --- | --- |
| Display | `cymonkey.display.inspect`, `.connect`, `.authorize`, `.call`, `.health`, `.events`, `.material`, `.disconnect` |
| Content | `cymonkey.content.start`, `.model.inspect`, `.observe`, `.interpret`, `.health`, `.capabilities`, `.close` |
| Device | `cymonkey.device.list`, `.open`, `.invoke`, `.stream.open`, `.stream.read`, `.stream.close`, `.close` |

A structured failure can retain a stable category:

```json
{
  "id": 2,
  "error": {
    "code": "rate_limit",
    "message": "plugin capacity is exhausted",
    "retryAfterMilliseconds": 2000
  }
}
```

The transport preserves the code. The Cymonkey domain adapter validates it and
maps the response into the public API.

## Extension boundary

A Cymonkey plugin implements one of the public contracts above and works
through Cymy. Plugin packages do not import Cymonkey's internal implementation
libraries or use their native protocol names.
