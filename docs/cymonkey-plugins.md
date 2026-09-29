# Executable provider plugins

Cymonkey links its built-in Jangolova, Blockade, and Board providers into the
binary. Optional providers can be installed as separate executables without
rebuilding Cymonkey. One installation, process transport, and version handshake
serves all three libraries; each library keeps its own operations.

## Install and select

Create a `plugin.json` beside an executable. For the current OS and CPU, a
Jangolova engine manifest looks like this (replace the digest with the real
SHA-256 of the executable):

```json
{
  "apiVersion": "provider.plugin/v1alpha1",
  "name": "my-display",
  "version": "1.0.0",
  "kind": "jangolova.engine",
  "platform": "darwin-arm64",
  "command": "provider",
  "sha256": "0000000000000000000000000000000000000000000000000000000000000000"
}
```

The shipped libraries recognize `jangolova.engine`, `blockade.provider`, and
`board.provider`. Each library defines its own `kind` identifier. The shared
installer accepts other dotted identifiers; such packages remain installed but
inactive until a corresponding library handler is registered.
`platform` is optional; when supplied it must match `GOOS-GOARCH`. `command`
is a filename in the manifest directory, without path separators. Plugin names
are lower-case identifiers. An installed name is also the Jangolova engine
name, Blockade provider-adapter kind, or Board provider ID. A name already used
by a built-in adapter cannot be registered again.
Previously installed packages with `cymonkey.plugin/v1alpha1` remain loadable;
new packages use the host-neutral `provider.plugin/v1alpha1` envelope.

```sh
cymonkey plugins install --manifest /path/to/plugin.json
cymonkey plugins list
cymonkey plugins upgrade --manifest /path/to/new/plugin.json
cymonkey plugins remove my-display
```

Install and upgrade read local files only. They verify the executable digest,
copy it into the user's Cymonkey plugin directory, and never run it during
installation. `CYMONKEY_PLUGIN_DIR` overrides that directory. Runtime discovery
rechecks the digest. An upgrade requires a new version with the same name and
kind, and retains the prior installation if staging fails. This release has no
remote catalog or automatic update command.

Jangolova plugins appear in `cymonkey provider engines` and may be selected
with `--adapter NAME`. Blockade plugins are selected by `kind: NAME` in a
provider-adapter configuration. Board plugins can be listed and invoked with:

```sh
cymonkey board provider-devices
cymonkey board provider-invoke --provider NAME --device DEVICE \
  --capability CAPABILITY --input '{}'
cymonkey board provider-stream --provider NAME --device DEVICE \
  --capability drive.read --resource ROOT --input '{}'
```

The Board command flags are the operator's explicit device and capability
selection. `board.Registry` checks the grant before a plugin session is opened
and before each action or stream. A Board plugin must also enforce the grant it
receives, including resource containment. Executable plugins run with the
installing user's OS permissions; Board grants are an application contract,
not an operating-system sandbox. Review a plugin before installing it.

## Process protocol

Cymonkey starts the installed executable directly, with no shell. It passes
JSON objects, one per line, on stdin and reads one response per line on stdout.
Stderr is for diagnostics. The host does not pass its full environment; it
supplies only `PATH` and, on Windows, `SystemRoot`. Provider-specific secrets
and connection material travel in explicit request fields.

```json
{"id":1,"method":"plugin.hello"}
{"id":1,"result":{"apiVersion":"provider.plugin/v1alpha1","name":"my-display","kind":"jangolova.engine"}}
```

The first call must be `plugin.hello`. Its version, name, and kind must match
the installed manifest. Calls are serialized per process; response IDs must
match requests. A response can use `"error":"message"`. Frames are bounded to
24 MiB. A canceled or timed-out call terminates its process. The handshake has
a five-second limit; ordinary calls have a 60-second default limit unless the
caller supplies a shorter deadline.

After the handshake, methods depend on `kind`:

| Kind | Methods |
| --- | --- |
| Jangolova | `jangolova.inspect`, `jangolova.connect`, `jangolova.authorize`, `jangolova.call`, `jangolova.health`, `jangolova.events`, `jangolova.material`, `jangolova.disconnect` |
| Blockade | `blockade.start`, `blockade.observe`, `blockade.health`, `blockade.capabilities`, `blockade.close` |
| Board | `board.list`, `board.open`, `board.invoke`, `board.stream.open`, `board.stream.read`, `board.stream.close`, `board.close` |

Jangolova uses the public `sdk.EngineSpec` and target fields for connect.
Endpoint connection snapshots include the authorized headers and revisions.
`jangolova.material` delivers later snapshots; the host acknowledges a
revision after the plugin accepts it. `jangolova.events` accepts a cursor and
returns `{ "cursor": "...", "events": [...] }` promptly; the host polls for
events while the session is open. The other results map to the public Jangolova
SDK inspection, authorization, health, capability, and caller-launch types.

Blockade starts with an adapter ID and non-secret settings. Each
`blockade.observe` request contains the versioned `ProviderAdapterObserveRequest`
and only the logical secrets configured for that adapter. Its response is a
`ProviderAdapterObserveResponse`; Blockade still validates the normalized
observation and request ID. Health and capabilities use Blockade's public
provider-adapter types.

Board `board.list` returns `[]board.Device`. `board.open` receives a device ID
and `board.Grant`. Invoke uses `board.Action` and returns `board.Result`.
`board.stream.open` returns `{ "id": "...", "size": 123, "truncated": false }`.
`board.stream.read` receives `{ "id": "...", "maxBytes": 32768 }` and returns
`{ "data": "<base64>", "eof": true }`. Each chunk is at most 32 KiB and the
total cannot exceed the declared size or 128 MiB. File bytes stay out of
`board.Result`; the CLI also applies its own stream limit.

The [fixture executable](../tests/plugin-fixture/main.go) implements all three
kinds for integration tests. The reusable process and installation code lives
in [`lib/plugin/`](../lib/plugin/), while each domain adapter lives in its own
library's `plugin` package. Cymonkey registers them at its executable edge;
`src/internal` does not import any of the three libraries.
