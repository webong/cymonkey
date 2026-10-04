# The `cymy` CLI

`cymy` is the public Cymonkey CLI. The existing `cymonkey` executable has the
same commands for deployment compatibility. The CLI can run a Cymonkey host,
coordinate observations, retrieve display modules, select browser targets,
manage caller-supplied extensions and userscripts, connect display adapters,
interpret content, and use approved devices.

## Build and use it

From the Cymonkey repository root:

```sh
mkdir -p .cache/bin
go build -o .cache/bin/cymy ./src
export PATH="$PWD/.cache/bin:$PATH"
cymy help
```

The checked-in example host manifest starts child processes named `cymonkey`,
so build that compatibility name as well when using the example unchanged:

```sh
go build -o .cache/bin/cymonkey ./src
```

Run commands from the repository root when using its relative example paths.
Most one-shot commands write JSON to standard output. Servers keep running
until interrupted. An error writes to standard error and exits nonzero.
`cymy help`, `cymy modules help`, `cymy display help`, and `cymy device help` show
the built-in summaries; `cymy userscript help` lists its lifecycle commands.
Other command groups are documented below; they do not all expose a
subcommand-specific help page.

## Command map

| Command | What it does |
| --- | --- |
| `cymy validate` | Check a Cymonkey host YAML manifest. |
| `cymy run` | Start and supervise the manifest's components. |
| `cymy observe` | Capture an authorized display screenshot and send it to the configured content service. |
| `cymy modules discover`, `pull` | Read a reviewed module registry and retrieve a selected artifact. |
| `cymy browser targets` | Find local browsers and profiles on the machine running the command. |
| `cymy operator serve`, `targets`, `connect`, `call`, `disconnect`, `observe`, `interpret`, `userscript`, `extension`, `device` | Keep browser sessions, content, and devices callable through one authenticated local API. |
| `cymy extension ...` | Inspect, package, stage, activate, or install a caller-supplied browser extension. |
| `cymy userscript ...` | Store, list, update, enable, disable, or remove extension-free userscripts for a selected browser target. |
| `cymy display adapters`, `connect`, `serve`, `serve-tools` | Discover or connect display adapters and serve display interaction APIs. |
| `cymy device devices`, `drive-list`, `drive-read` | List host-approved mounted drives, list entries, or stream one selected file. |
| `cymy device keyboard-press`, `keyboard-type` | Send a key or bounded text to an explicitly selected macOS process. |
| `cymy device adapter-devices`, `adapter-invoke`, `adapter-stream` | Use installed device plugins under an explicit grant. |
| `cymy content validate`, `runtime manifest/install/list/remove`, `model install/list/remove`, `observe`, `interpret`, `serve` | Validate content configuration, manage runtimes and models, request inference, or serve the content API. |
| `cymy plugins manifest`, `install`, `upgrade`, `list`, `remove` | Build and manage display, content, and device plugin packages. |

## Device adapters

A host names an already mounted directory with `--root NAME=PATH`. Cymonkey
publishes `NAME` as an opaque resource handle; action results never expose the
host path. The list is bounded, and the read command refuses files larger than
its stream limit before writing anything to stdout.

```sh
cymy device devices --root photos=/Volumes/CAMERA
cymy device drive-list --root photos=/Volumes/CAMERA --path DCIM --recursive
cymy device drive-read --root photos=/Volumes/CAMERA \
  --path DCIM/100CANON/IMG_0001.JPG > photo.jpg
```

Each command creates a fresh attachment. The host path must still refer to the
approved directory for every action. Drive reads are read-only and limited to
32 MiB by default; `--max-bytes` can tighten that bound. `drive-list` accepts
`--limit`, `--recursive`, and `--json`.

On macOS, grant Accessibility permission to the terminal or host process,
choose an existing process ID, then submit a key or text. Text comes from
stdin, so it does not appear in the command line. `keyboard-type` accepts at
most 1,024 Unicode code points and no control characters; use
`keyboard-press` for Enter, Tab, and shortcuts.

```sh
cymy device keyboard-press --pid 12345 --key Command+O
printf 'photo caption' | cymy device keyboard-type --pid 12345
cymy device keyboard-press --pid 12345 --key Enter
```

Both keyboard commands return `{"submitted":true}` when macOS accepted the
events for posting. The target application may ignore them. The adapter refuses
a process that has exited or changed identity. These commands require macOS
with cgo; other builds report that the adapter is unavailable.

## Host and coordinated observation

All three host commands accept `--config PATH`. You can set
`CYMONKEY_CONFIG` instead. A config path is required either way.

```sh
cymy validate --config scripts/deploy/cymonkey/cymonkey.example.yaml

export CYMONKEY_DISPLAY_TOKEN='a-random-session-secret'
cymy run --config scripts/deploy/cymonkey/cymonkey.example.yaml
```

`validate` reports the number of configured components. `run` starts the
manifest's component commands in order and supervises them until interrupted
or a component exits. The example manifest requires the `cymy` executable
on `PATH` for those child commands, as shown in the build section.

With that host running and a display interaction instance already connected,
run this from another terminal:

```sh
export CYMONKEY_DISPLAY_TOKEN='a-random-session-secret'
cymy observe --config scripts/deploy/cymonkey/cymonkey.example.yaml \
  --instance browser-1 --prompt 'find the primary action'
```

`--instance` is required. `--prompt TEXT` is optional. `--full-page` asks for a
full browser-page capture, and `--approval-id ID` supplies a display
screenshot approval when the instance's policy requires one. The response is
a JSON observation envelope. The token must match the running display service's
token. See the [host guide](cymonkey-host.md) for the composition boundary.

## Reviewed modules

```sh
cymy modules discover --registry https://registry.example/display-modules.json

cymy modules pull \
  --registry https://registry.example/display-modules.json \
  --module render/blender --platform linux-amd64 \
  --cache /absolute/path/to/module-cache
```

`discover` returns registry metadata as JSON. `pull` selects the named module
and platform, retrieves its artifact, verifies it, and returns its cached
path as JSON. The registry must use HTTPS except for loopback development
endpoints. `pull` does not activate a module. See the
[registry guide](display-module-registry.md).

## Browsers and extensions

For a persistent browser session that other tools can call, use
[`cymy operator serve`](operator-api.md). It accepts a discovered target ID or
explicit browser/profile paths, uses an existing endpoint when available, and
keeps the interaction and manager API alive.

```sh
cymy browser targets
cymy extension capabilities --browser firefox
cymy extension prepare --source /absolute/path/to/signed-extension.xpi
```

`browser targets` returns JSON entries with IDs for discovered browser
profiles. Discovery runs on the machine executing `cymy`. `extension
capabilities` reports the selected browser's installation modes and support;
it accepts `--browser NAME` or a discovered `--target ID`. `prepare` and its
alias `inspect` describe the supplied files and return a `revision`. Use the
exact returned revision when installing or staging those same files.

| Extension command | Main flags and result |
| --- | --- |
| `prepare`, `inspect` | `--source PATH`; inspect a directory, ZIP, signed XPI, or Safari app and return its revision. |
| `package` | `--source DIRECTORY --output ZIP`; create a deterministic ZIP. |
| `sign` | `--browser firefox|chrome|chromium|edge --source PATH --revision VALUE --output PATH`; use the caller's Mozilla account or local Chromium executable to produce a signed XPI or CRX. Chromium accepts `--target ID` or `--browser-bin PATH`, and optional `--key PATH` to retain its extension ID. |
| `package-safari` | `--source PATH --output DIRECTORY --revision VALUE --bundle-id ID --app-name NAME`; generate a caller-owned Xcode project. |
| `build-safari` | `--project XCODEPROJ --scheme NAME --team-id ID --output APP`; build and sign the caller-owned Safari app with their local Xcode team. |
| `capabilities` | `--browser NAME` or `--target ID`; report what that browser supports. |
| `install` | `--target ID` or explicit browser/profile paths, plus `--source PATH --revision VALUE`; perform the browser-specific installation workflow. Chromium may also need `--destination DIRECTORY`. |
| `run` | Same target, source, and revision selection as `install`; load the extension in a controlled browser session. `--headless` defaults to true; use `--headless=false` for a visible session. The reported state is `activated` for that session. |
| `stage` | `--browser NAME` or `--target ID`, with `--source PATH --destination DIRECTORY --revision VALUE`; prepare an unpacked Chromium extension for manual loading. |
| `install-store` | `--browser chrome|edge --id STORE_ID [--store chrome|edge] [--external-dir DIRECTORY]`; request a published store extension through the browser's external-install mechanism. Windows uses PowerShell and the machine registry; macOS/Linux use documented JSON locations. |
| `remove-store` | The same selection flags as `install-store`; remove the matching external-install request. This does not remove an independently installed extension. |
| `policy` | `--browser chrome|edge --id STORE_ID --mode force|unforce|block|unblock [--store chrome|edge]`; change administrator-owned force-install or block policies on Windows, or Chrome managed policy on Linux. |
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
`extension.prepare`, `extension.package`, `extension.sign`, `extension.package-safari`, `extension.build-safari`,
`extension.install`, `extension.stage`, `extension.install-store`, and
`extension.remove-store`, and `extension.policy`. Its
`--input` is one JSON object. For example:

```sh
cymy extension act --name extension.targets --input '{}'
cymy extension act --name extension.prepare \
  --input '{"source":"/absolute/path/to/signed-extension.xpi"}'
```

`extension.install` and `extension run` can emit multiple JSON lines and remain
active while their browser session runs. Installation behavior differs by
browser and profile. Follow the [step-by-step extension CLI guide](cymy-extension-cli.md)
for Firefox, Chromium, and Safari examples and the meaning of `installed`,
`activated`, and `awaiting-browser-action`. The
[extension manager reference](browser-extension-installation.md) covers
platform limits.

## Extension-free userscripts

For a userscript with `@name` and `@match` metadata, one command stores it and
attaches a display adapter to a browser endpoint:

```sh
cymy userscript install --source /absolute/path/to/reading-tools.user.js \
  --endpoint cdp=http://127.0.0.1:9222
```

Leave the command running while the script should remain active. It prints a
`stored` record with the chosen target ID, followed by a `connected` record.
Use that target ID with `list`, `disable`, or `uninstall`. Without `--target`, an
endpoint gets a stable key derived from its address; pass your own stable
`--target` when the endpoint address might change.

Without `--endpoint`, `install` only stores the script. If exactly one compatible
local browser profile is discovered, Cymonkey selects its ID automatically.
With multiple profiles, select one from `cymy browser targets` using `--target`.
When attaching later, the caller must connect the endpoint that actually
belongs to that profile.

If a script has no metadata block, provide `--match`; Cymonkey derives its name
and ID from the filename. `--name` and `--id` override the inferred values.
The `install` command itself approves the exact local file and URL scope.
An optional `prepare` / `--revision` sequence lets a caller review and pin
those bytes and patterns before installing.

Prepare a local JavaScript file to see its source-free description and a
SHA-256 review revision bound to the source, target ID, name, and URL patterns:

```sh
cymy userscript prepare --target 'chrome:ID_FROM_TARGETS' \
  --id reading-tools --name 'Reading Tools' \
  --source /absolute/path/to/reading-tools.js \
  --match 'https://example.com/*'
```

Install the same file with the returned revision when a separate review is
needed. This copies the source into Cymonkey's private local store; the file
can then be moved or removed. An untrusted tool must not be given unrestricted
access to this local CLI or store.

```sh
cymy userscript install --target 'chrome:ID_FROM_TARGETS' \
  --id reading-tools --name 'Reading Tools' \
  --source /absolute/path/to/reading-tools.js \
  --match 'https://example.com/*' \
  --revision 'sha256:VALUE_FROM_PREPARE'
```

The default store is the operating system user config directory under
`cymonkey/userscripts`. Use `--store /absolute/path/to/store` with each command
to select another directory. A script is stored for one target ID; it is
registered in the browser when Cymonkey attaches to that target:

```sh
cymy display connect --target-kind browser \
  --endpoint cdp=http://127.0.0.1:9222 \
  --userscripts-target 'chrome:ID_FROM_TARGETS'
```

Pass `--userscripts-store` to `connect` if you used a custom store.
The display adapter registers each enabled script for matching future documents. While
the connection runs, Cymonkey checks the store every two seconds and applies
changes. After the browser closes or Cymonkey disconnects, start
`connect` again to reapply the stored scripts. The script registration
itself lasts only for the connected session. A newly opened tab can load before
the display adapter attaches; in that case it applies the script to the current document
when attachment completes. Later navigations use document-start injection.

| Command | Flags and effect |
| --- | --- |
| `userscript prepare` | `--source`; infer metadata and target when possible. Inspect without storing. |
| `userscript install` | `--source`; optional `--endpoint PROTOCOL=URL` also attaches the browser. Accepts an optional exact `--revision` from `prepare`. |
| `userscript update` | Same as `install`; replace an installed script while preserving its enabled state. |
| `userscript list` | `--target`; return source-free records. |
| `userscript describe` | `--target --id`; return one source-free record. |
| `userscript enable`, `disable` | `--target --id`; change what the display adapter registers. |
| `userscript uninstall` | `--target --id`; remove the stored source and stop future registration. |

Commands accept `--target` and `--store`; install, update, and prepare also
accept `--id`, `--name`, repeated `--match` and `--exclude-match`. The
extension-free backend supports
scripts up to 1 MiB, `@grant none`, page-world execution at document start,
and URL match/exclusion patterns. It does not provide Greasemonkey/Tampermonkey
`GM_*` grants, `@require`, `@resource`, isolated `USER_SCRIPT` world, or
browser-native persistence while Cymonkey is offline. Metadata such as
`@include`, `@exclude`, and automatic update URLs is also unsupported. If the
file has a userscript metadata block, its match patterns must agree with the
flags.
Disabling or uninstalling prevents future execution; it cannot undo changes
already made to an open page. See [userscripts](userscripts.md).

## Bookmarklets

`cymy bookmarklet export --source tool.js` prints a portable `javascript:` URL.
Add `--format html --name 'My tool'` to generate a draggable installation page.
`cymy bookmarklet import --source bookmark.txt` decodes an existing URL to source
for review. Both commands write to stdout and neither executes source nor edits
browser bookmarks. See [bookmarklets](bookmarklets.md) for installation, the
reversible highlighter example, compatibility limits and live tests.

## Display interaction

List the registered display adapters, their availability, and their
capabilities:

```sh
cymy display adapters
cymy display adapters --json
```

Attach to an existing caller-owned target with `connect`. The command
reports a connected instance as JSON, then remains attached until the
connection ends or you interrupt it. For a browser exposing CDP:

```sh
cymy display connect --target-kind browser \
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
`--userscripts-target ID` replays and watches that target's stored scripts;
`--userscripts-store DIRECTORY` selects a non-default store.

Run the display service for other tools:

```sh
export CYMONKEY_DISPLAY_TOKEN='a-random-session-secret'
cymy display serve --bind 127.0.0.1:7391
```

The HTTP service defaults to `127.0.0.1:7391`; `CYMONKEY_DISPLAY_BIND` can
set its default bind address. `CYMONKEY_DISPLAY_TOKEN` is required.
`serve-tools` exposes display tools over stdio by default, or Streamable
HTTP with `--bind ADDRESS`:

```sh
cymy display serve-tools
cymy display serve-tools --bind 127.0.0.1:7393
```

Both MCP forms also require `CYMONKEY_DISPLAY_TOKEN`. See the
[display service guide](engine-provider.md) for its operations and
[target connection security](target-connection-security.md) for reference
handling. A connected display adapter can expose `script.execute` and
`script.register` through CDP or WebDriver BiDi without installing an
extension; those registrations are tied to the browser connection. Cymonkey
provides libraries for another extension to implement a browser-native
userscript manager. See [userscripts](userscripts.md) and
[browser augmentation packages](browser-augmentation-packages.md).

## Content interpretation

```sh
cymy content validate --config scripts/deploy/content/content.example.yaml
cymy content validate --config scripts/deploy/content/content.example.yaml --check-files
```

`validate` checks the Cymonkey content YAML. `--check-files` additionally verifies the
digests of installed plugin models. A referenced local model must be
installed before that check or inference can succeed.

Observe a PNG or JPEG file with a configured inference backend:

```sh
cymy content observe --config /absolute/path/to/content.yaml \
  --image /absolute/path/to/screenshot.png \
  --inference local-yolo-sam --prompt 'find visible controls'
```

`--config` and `--image` are required; `--prompt` is optional. Omit
`--inference` to use the config's first adapter. The result is JSON.

Interpret text or audio with a configured model adapter:

```sh
cymy content interpret --config /absolute/path/to/content.yaml \
  --inference content-model --text-file note.txt --prompt 'Summarize'
cymy content interpret --config /absolute/path/to/content.yaml \
  --inference content-model --audio recording.wav --audio-type audio/wav \
  --prompt 'Transcribe speech'
```

Install, list, and remove a plugin-owned local model:

```sh
cymy content model install --manifest /absolute/path/to/model.json \
  --config /absolute/path/to/content.yaml
cymy content model list
cymy content model remove --plugin onnx --model yolo11n@1.0.0
```

The public manifest uses `cymonkey.model/v1alpha1`, names the installed plugin,
and includes every artifact's role and SHA-256 digest. The content adapter
copies and verifies the files, asks that plugin to inspect compatibility, then registers the installed
version. Use `--root` on model commands or `CYMONKEY_MODEL_DIR` to choose a
different model registry.

Install, list, and remove a managed runtime for source plugins:

```sh
cymy content runtime manifest --source /absolute/path/to/python-tree \
  --name python --version 3.12.8 --executable bin/python3 \
  --output /absolute/path/to/python-tree/runtime.json
cymy content runtime install --manifest /absolute/path/to/runtime.json
cymy content runtime list
cymy content runtime remove --name python --version 3.12.8
```

`manifest` inventories a self-contained runtime tree and records all digests.
Use `--root` or `CYMONKEY_RUNTIME_DIR` to choose the runtime registry. Runtime
files and the selected executable are digest checked; Cymonkey does not use an
ambient executable from `PATH`.

`interpret` also accepts `--text` and `--image`; combine content flags only
when the plugin advertises `multimodal.interpret`. The selected plugin must
advertise the requested modality. Cymonkey does not bundle a
text or audio model.

Start the HTTP content service with the same config and adapter selection:

```sh
cymy content serve --config /absolute/path/to/content.yaml \
  --inference local-yolo-sam --bind 127.0.0.1:8091
```

`serve` defaults to `127.0.0.1:8091`, or the value of `CYMONKEY_CONTENT_BIND`.
See the [content guide](content.md) for adapter configuration and the
difference between direct image observation and `cymy observe`.
