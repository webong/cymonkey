# The `cmy` CLI

`cmy` is the Cymonkey executable built with a short name. It has the same
commands as `cymonkey`. It can run a Cymonkey host, coordinate visual
observations, discover and retrieve Jangolova modules, select browser targets,
manage caller-supplied extensions, connect interaction engines, and run the
Jangolova and Blockade services.

## Build and use it

From the Cymonkey repository root:

```sh
mkdir -p .cache/bin
go build -o .cache/bin/cmy ./src
export PATH="$PWD/.cache/bin:$PATH"
cmy help
```

The executable name is your choice. The checked-in example host manifest
starts child processes named `cymonkey`, so build that name as well when using
the example unchanged. Built-in help also prints `cymonkey` as the executable
name:

```sh
go build -o .cache/bin/cymonkey ./src
```

Run commands from the repository root when using its relative example paths.
Most one-shot commands write JSON to standard output. Servers keep running
until interrupted. An error writes to standard error and exits nonzero.
`cmy help`, `cmy modules help`, and `cmy provider help` show the built-in
summaries. Other command groups are documented below; they do not all expose a
subcommand-specific help page.

## Command map

| Command | What it does |
| --- | --- |
| `cmy validate` | Check a Cymonkey host YAML manifest. |
| `cmy run` | Start and supervise the manifest's components. |
| `cmy observe` | Capture an authorized Jangolova screenshot and send it to Blockade for observation. |
| `cmy modules discover`, `pull` | Read a reviewed module registry and retrieve a selected artifact. |
| `cmy browser targets` | Find local browsers and profiles on the machine running the command. |
| `cmy extension ...` | Inspect, package, stage, activate, or install a caller-supplied browser extension. |
| `cmy provider engines`, `connect-engine`, `serve-engine-provider`, `serve-mcp` | Discover or connect interaction engines, or run Jangolova interfaces. |
| `cmy blockade validate`, `observe`, `serve` | Validate visual inference configuration, observe an image, or run the inference service. |
| `cmy native-bridge-fixture` | Run the native bridge test fixture; this is for development. |

## Host and coordinated observation

All three host commands accept `--config PATH`. You can set
`CYMONKEY_CONFIG` instead. A config path is required either way.

```sh
cmy validate --config infra/deploy/cymonkey/cymonkey.example.yaml

export CYMONKEY_JANGOLOVA_TOKEN='a-random-session-secret'
cmy run --config infra/deploy/cymonkey/cymonkey.example.yaml
```

`validate` reports the number of configured components. `run` starts the
manifest's component commands in order and supervises them until interrupted
or a component exits. The example manifest requires the `cymonkey` executable
on `PATH` for those child commands, as shown in the build section.

With that host running and a Jangolova interaction instance already connected,
run this from another terminal:

```sh
export CYMONKEY_JANGOLOVA_TOKEN='a-random-session-secret'
cmy observe --config infra/deploy/cymonkey/cymonkey.example.yaml \
  --instance browser-1 --prompt 'find the primary action'
```

`--instance` is required. `--prompt TEXT` is optional. `--full-page` asks for a
full browser-page capture, and `--approval-id ID` supplies a Jangolova
screenshot approval when the instance's policy requires one. The response is
a JSON observation envelope. The token must match the running provider's
token. See the [host guide](cymonkey-host.md) for the composition boundary.

## Reviewed modules

```sh
cmy modules discover --registry https://registry.example/jangolova.json

cmy modules pull \
  --registry https://registry.example/jangolova.json \
  --module render/blender --platform linux-amd64 \
  --cache /absolute/path/to/module-cache
```

`discover` returns registry metadata as JSON. `pull` selects the named module
and platform, retrieves its artifact, verifies it, and returns its cached
path as JSON. The registry must use HTTPS except for loopback development
endpoints. `pull` does not activate a module. See the
[registry guide](jangolova-module-registry.md).

## Browsers and extensions

```sh
cmy browser targets
cmy extension capabilities --browser firefox
cmy extension prepare --source /absolute/path/to/signed-extension.xpi
```

`browser targets` returns JSON entries with IDs for discovered browser
profiles. Discovery runs on the machine executing `cmy`. `extension
capabilities` reports the selected browser's installation modes and support;
it accepts `--browser NAME` or a discovered `--target ID`. `prepare` and its
alias `inspect` describe the supplied files and return a `revision`. Use the
exact returned revision when installing or staging those same files.

| Extension command | Main flags and result |
| --- | --- |
| `prepare`, `inspect` | `--source PATH`; inspect a directory, ZIP, signed XPI, or Safari app and return its revision. |
| `package` | `--source DIRECTORY --output ZIP`; create a deterministic ZIP. This does not sign an XPI or build a Safari app. |
| `package-safari` | `--source PATH --output DIRECTORY --revision VALUE --bundle-id ID --app-name NAME`; generate a caller-owned Xcode project. |
| `capabilities` | `--browser NAME` or `--target ID`; report what that browser supports. |
| `install` | `--target ID` or explicit browser/profile paths, plus `--source PATH --revision VALUE`; perform the browser-specific installation workflow. Chromium may also need `--destination DIRECTORY`. |
| `run` | Same target, source, and revision selection as `install`; load the extension in a controlled browser session. `--headless` defaults to true; use `--headless=false` for a visible session. The reported state is `activated` for that session. |
| `stage` | `--browser NAME` or `--target ID`, with `--source PATH --destination DIRECTORY --revision VALUE`; prepare an unpacked Chromium extension for manual loading. |
| `install-store` | `--id CHROME_WEB_STORE_ID --external-dir DIRECTORY`; write a Chrome External Extensions install request where that deployment method is supported. |
| `act` | `--name ACTION --input JSON`; call the local structured extension manager interface. |

For `install` and `run`, a discovered `--target ID` cannot be mixed with the
explicit selection flags. Explicit selection uses `--browser NAME`, optional
`--browser-bin ABSOLUTE_PATH`, `--profile ABSOLUTE_PATH`, and, for Chromium
browsers, `--profile-directory NAME`. Supported browser names are `chrome`,
`chromium`, `edge`, `firefox`, and `safari`. `--profile` means the Chromium
user data directory for Chrome/Chromium/Edge and the exact profile directory
for Firefox. Safari profile enablement happens in Safari Settings. Native
installation requires a visible browser; `install --headless=true` is
rejected.

`act` accepts these names: `extension.targets`, `extension.capabilities`,
`extension.prepare`, `extension.package`, `extension.package-safari`,
`extension.install`, `extension.stage`, and `extension.install-store`. Its
`--input` is one JSON object. For example:

```sh
cmy extension act --name extension.targets --input '{}'
cmy extension act --name extension.prepare \
  --input '{"source":"/absolute/path/to/signed-extension.xpi"}'
```

`extension.install` and `extension run` can emit multiple JSON lines and remain
active while their browser session runs. Installation behavior differs by
browser and profile. Follow the [step-by-step extension CLI guide](cmy-extension-cli.md)
for Firefox, Chromium, and Safari examples and the meaning of `installed`,
`activated`, and `awaiting-browser-action`. The
[extension manager reference](browser-extension-installation.md) covers
platform limits.

## Jangolova interaction provider

List the registered interaction engines, their availability, and their
capabilities:

```sh
cmy provider engines
cmy provider engines --json
```

Attach to an existing caller-owned target with `connect-engine`. The command
reports a connected instance as JSON, then remains attached until the
connection ends or you interrupt it. For a browser exposing CDP:

```sh
cmy provider connect-engine --target-kind browser \
  --endpoint cdp=http://127.0.0.1:9222
```

`--target-kind KIND` is required. `--adapter NAME` defaults to `auto`;
`--require-capability NAME` can be repeated to constrain automatic selection.
`--endpoint PROTOCOL=URL`, `--handle NAME=VALUE`, `--credential-ref
NAME=REFERENCE`, and `--tls-ref NAME=REFERENCE` can each be repeated to
describe the supplied target. `--source RESOURCE` is an optional resource to
present after connecting. `--driver NAME` selects a control-plane driver;
`--options JSON` supplies adapter-specific options as a JSON object.
`--disconnect-timeout DURATION` defaults to `15s`.

Run Jangolova as a service for other tools:

```sh
export JANGOLOVA_PROVIDER_TOKEN='a-random-session-secret'
cmy provider serve-engine-provider --bind 127.0.0.1:7391
```

The HTTP provider defaults to `127.0.0.1:7391`; `JANGOLOVA_PROVIDER_BIND` can
set its default bind address. `JANGOLOVA_PROVIDER_TOKEN` is required.
`serve-mcp` exposes the provider tools over stdio by default, or Streamable
HTTP with `--bind ADDRESS`:

```sh
cmy provider serve-mcp
cmy provider serve-mcp --bind 127.0.0.1:7393
```

Both MCP forms also require `JANGOLOVA_PROVIDER_TOKEN`. See the
[interaction provider guide](engine-provider.md) for its operations and
[target connection security](target-connection-security.md) for reference
handling. A connected Jangolova provider can expose `script.execute` and
`script.register` through CDP or WebDriver BiDi without installing an
extension; those registrations are tied to the browser connection. Cymonkey's
current managed `userscript.prepare/install/update/enable` lifecycle uses its
WebExtension for approval, storage, and registration. These are distinct
paths; see [userscripts](userscripts.md) and
[browser augmentation packages](browser-augmentation-packages.md).

## Blockade visual inference

```sh
cmy blockade validate --config infra/deploy/blockade/blockade.example.yaml
cmy blockade validate --config infra/deploy/blockade/blockade.example.yaml --check-files
```

`validate` checks the Blockade YAML. `--check-files` additionally checks
referenced local model files. The example model paths need real models before
that check or inference can succeed.

Observe a PNG or JPEG file with a configured inference backend:

```sh
cmy blockade observe --config /absolute/path/to/blockade.yaml \
  --image /absolute/path/to/screenshot.png \
  --inference local-yolo-sam --prompt 'find visible controls'
```

`--config` and `--image` are required; `--prompt` is optional. Omit
`--inference` to use the config's default backend. `--engine` remains a
deprecated alias for `--inference`; supply only one of them. The result is
JSON.

Start the HTTP inference service with the same config and backend selection:

```sh
cmy blockade serve --config /absolute/path/to/blockade.yaml \
  --inference local-yolo-sam --bind 127.0.0.1:8091
```

`serve` defaults to `127.0.0.1:8091`, or the value of `BLOCKADE_BIND`.
See the [Blockade guide](blockade.md) for backend configuration and the
difference between direct image observation and `cmy observe`.

## Developer fixture

`cmy native-bridge-fixture` runs a cooperative native engine used by bridge
integration tests. It expects `JANGOLOVA_BRIDGE_URL`,
`JANGOLOVA_BRIDGE_TOKEN`, and `JANGOLOVA_BRIDGE_PROTOCOL` from the test
harness. It is not a general-purpose browser or extension command.
