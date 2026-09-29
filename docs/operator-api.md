# Cymonkey operator API

`cmy operator serve` keeps browser interaction sessions alive and exposes one
authenticated HTTP surface for callers. The operator uses Jangolova for display
actions, Blockade for configured image observations, and installed Board
providers for device operations. Browser extensions and userscripts remain
caller supplied.

## Start and connect

Build the CLI, set a secret, and select an existing browser target:

```sh
go build -o .cache/bin/cmy ./src
export CYMONKEY_OPERATOR_TOKEN='replace-with-a-random-secret'
.cache/bin/cmy browser targets
.cache/bin/cmy operator serve --target 'chromium:ID_FROM_TARGETS'
```

If that Chromium profile has an active `DevToolsActivePort` marker, Cymonkey
uses its local CDP endpoint. Otherwise supply an endpoint from the target
provider. Firefox, Safari, and WebDriver sessions require one explicitly:

```sh
.cache/bin/cmy operator serve --target 'firefox:ID_FROM_TARGETS' \
  --endpoint webdriver-bidi=ws://127.0.0.1:9223/session
```

The server prints a JSON interaction instance and stays running on
`127.0.0.1:7395`. From another terminal with the same token:

```sh
.cache/bin/cmy operator call --instance INSTANCE_ID \
  --name window.screenshot --input '{}'
```

To enable image observations, start Blockade separately and add
`--blockade-url http://127.0.0.1:8091` to `operator serve`. A Cymonkey host
manifest with an observation component can also be supplied through `--config`.

For an already running operator, use `cmy operator targets` and
`cmy operator connect --target ID [--endpoint PROTOCOL=URL]` instead. To point
at a custom installation, use `--browser`, `--browser-bin`, `--profile`, and
`--profile-directory` with `serve` or `connect`. With no target or endpoint,
connection selects the only discovered Chromium profile with an active
debugging marker; multiple candidates require an explicit target.

The same CLI reaches the operator's managers:

```sh
.cache/bin/cmy operator userscript install --target TARGET_ID \
  --source /absolute/path/to/script.user.js
.cache/bin/cmy operator extension --name extension.prepare \
  --input '{"source":"/absolute/path/to/extension.zip"}'
.cache/bin/cmy operator board devices
.cache/bin/cmy operator board invoke --provider DEVICE_PROVIDER --device DEVICE_ID \
  --capability drive.list --resource APPROVED_ROOT --input '{}'
.cache/bin/cmy operator board stream --provider DEVICE_PROVIDER --device DEVICE_ID \
  --capability drive.read --resource APPROVED_ROOT --input '{"path":"photo.jpg"}' > photo.jpg
```

Profile discovery never starts a browser or enables remote debugging. An
explicit endpoint is trusted as caller supplied; Cymonkey cannot prove it
belongs to the selected profile. A CDP endpoint can expose other profiles in
the same browser process, so profile selection is not an isolation boundary.
Use the interaction policy and approvals to limit actions.

## HTTP routes

Every route except `GET /healthz` requires
`Authorization: Bearer $CYMONKEY_OPERATOR_TOKEN`. The server binds to loopback;
use an authenticated HTTPS proxy if another machine needs access. File paths in
extension and userscript requests refer to files on the operator's machine.

| Route | Purpose |
| --- | --- |
| `GET /v1/browser-targets` | Discover local browser installations and profiles. |
| `POST /v1/browser-sessions` | Select a browser/profile, resolve an existing endpoint, and create a callable session. |
| `GET /v1/engines`, `POST /v1/instances`, `GET/DELETE /v1/instances/{id}`, `POST /v1/instances/{id}/call` | Existing Jangolova discovery, connection, action, and disconnect operations. Approval, event, and reconcile routes remain available. |
| `POST /v1/userscripts/{prepare,install,update,list,describe,enable,disable,uninstall}` | Store and manage approved local userscripts; apply changes to matching operator browser sessions. |
| `POST /v1/extensions/actions` | Run the structured extension manager actions. `extension.install` and `extension.run` require a prepared revision and stream newline-delimited JSON until the browser flow ends. |
| `POST /v1/observations` | Capture from an operator session and send the image to Blockade. Start with `--blockade-url` or `--config` and a running Blockade service. |
| `GET /v1/board/devices` | List devices from installed Board provider executables. |
| `POST /v1/board/actions` | Invoke a Board action with an explicit device and capability grant; returns JSON. |
| `POST /v1/board/streams` | Read bounded Board content with an explicit grant; returns bytes, up to 16 MiB. |

Connect with a discovered target ID or an explicit endpoint:

```json
{
  "instanceId": "browser-one",
  "targetId": "chromium:ID_FROM_TARGETS",
  "endpoint": "cdp=http://127.0.0.1:9222",
  "adapter": "auto",
  "approval": {"requiredActions": ["window.screenshot"]}
}
```

`endpoint` is optional when an active Chromium debugging marker exists. A
request can instead specify `browser`, `browserBin`, `profile`, and
`profileDirectory`; do not combine these with `targetId`. `userscriptsTarget`
selects a separate stable userscript key for an explicit connection. The
operator replays stored scripts when it connects a session and applies changes
made through its userscript routes while that session is active. Scripts still
need an active browser connection; they do not become browser-native installs.

Extension actions use the same names and input objects as `cmy extension act`:

```json
{"name":"extension.prepare","input":{"source":"/absolute/path/to/extension.zip"}}
```

For a managed image observation:

```json
{"instanceId":"browser-one","prompt":"find the primary action"}
```

Interaction calls reuse the provider's capability policy, action approvals,
and audit events. Extension and userscript management requires the operator
token and a local source path; extension installation also requires the exact
prepared revision. These manager actions do not yet have separate approval
receipts. Browser installation still follows each browser's native signature
and consent rules.

Board requests use the public Board contract. For example, the body of
`POST /v1/board/streams` can be:

```json
{"open":{"providerId":"camera-drive","deviceId":"camera-1","grant":{"capabilities":["drive.read"],"resourceIds":["photos"]}},"action":{"capability":"drive.read","resourceId":"photos","input":{"path":"DCIM/photo.jpg"}}}
```

The operator requires one requested capability per grant. Board checks the
grant before opening and streaming. The provider must also enforce the
resource boundary it receives. These routes currently load installed Board
provider executables; Board's built-in mounted-drive and macOS keyboard
commands remain available through `cmy board`. The caller holding the operator
token can request Board grants, so share that token only with authorized callers.
