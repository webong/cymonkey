# Cymonkey operator API

`cymy operator serve` keeps browser interaction sessions alive and exposes one
authenticated HTTP surface for callers. It composes Cymonkey's display,
content, and device contracts. Browser extensions and userscripts remain caller
supplied.

## Start and connect

Build the CLI, set a secret, and select an existing browser target:

```sh
go build -o .cache/bin/cymy ./src
export CYMONKEY_OPERATOR_TOKEN='replace-with-a-random-secret'
.cache/bin/cymy browser targets
.cache/bin/cymy operator serve --target 'chromium:ID_FROM_TARGETS'
```

If that Chromium profile has an active `DevToolsActivePort` marker, Cymonkey
uses its local CDP endpoint. Otherwise supply an endpoint from the target
provider. Firefox, Safari, and WebDriver sessions require one explicitly:

```sh
.cache/bin/cymy operator serve --target 'firefox:ID_FROM_TARGETS' \
  --endpoint webdriver-bidi=ws://127.0.0.1:9223/session
```

The server prints a JSON interaction instance and stays running on
`127.0.0.1:7395`. From another terminal with the same token:

```sh
.cache/bin/cymy operator call --instance INSTANCE_ID \
  --name window.screenshot --input '{}'
```

To enable image observations and content interpretation, start the content
service and bind it to the operator:

```sh
.cache/bin/cymy operator serve \
  --service content.interpretation=http://127.0.0.1:8091
```

Use `--content-url URL` for the same direct binding. An authenticated remote
content service also uses `--content-token-env NAME`; the operator reads the
token from that environment variable and forwards it as a bearer credential.
A Cymonkey host manifest with a `content.interpretation` service can instead be
supplied through `--config`.

For an already running operator, use `cymy operator targets` and
`cymy operator connect --target ID [--endpoint PROTOCOL=URL]` instead. To point
at a custom installation, use `--browser`, `--browser-bin`, `--profile`, and
`--profile-directory` with `serve` or `connect`. With no target or endpoint,
connection selects the only discovered Chromium profile with an active
debugging marker; multiple candidates require an explicit target.

The same CLI reaches the operator's managers:

```sh
.cache/bin/cymy operator userscript install --target TARGET_ID \
  --source /absolute/path/to/script.user.js
.cache/bin/cymy operator extension --name extension.prepare \
  --input '{"source":"/absolute/path/to/extension.zip"}'
.cache/bin/cymy operator device devices
.cache/bin/cymy operator interpret --text-file /absolute/path/to/note.txt \
  --prompt 'Summarize this'
.cache/bin/cymy operator interpret --audio /absolute/path/to/recording.wav \
  --audio-type audio/wav --prompt 'Transcribe speech'
.cache/bin/cymy operator device invoke --adapter DEVICE_ADAPTER --device DEVICE_ID \
  --capability drive.list --resource APPROVED_ROOT --input '{}'
.cache/bin/cymy operator device stream --adapter DEVICE_ADAPTER --device DEVICE_ID \
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
| `GET /v1/display-adapters`, `POST /v1/display-instances`, `GET/DELETE /v1/display-instances/{id}`, `POST /v1/display-instances/{id}/call` | Display adapter discovery, connection, action, and disconnect operations. Approval, event, and reconcile routes remain available. |
| `POST /v1/userscripts/{prepare,install,update,list,describe,enable,disable,uninstall}` | Store and manage approved local userscripts; apply changes to matching operator browser sessions. |
| `POST /v1/extensions/actions` | Run structured extension manager actions, including `extension.sign`, `extension.build-safari`, `extension.install-store`, `extension.remove-store`, and `extension.policy`. Store requests use the operator machine's PowerShell registry (Windows) or documented external preferences directory (macOS/Linux). Administrator policies use Windows PowerShell or managed Chrome JSON on Linux. Signing uses the operator machine's browser/Xcode tools or Mozilla credentials. `extension.install` and `extension.run` require a prepared revision and stream newline-delimited JSON until the browser flow ends. |
| `POST /v1/observations` | Capture from an operator session and send the image to the configured content service. |
| `POST /v1/interpretations` | Forward caller-supplied text, image, audio, or video to a capable content adapter; returns source-linked findings. |
| `GET /v1/devices` | List devices from installed device plugins. |
| `POST /v1/device-actions` | Invoke an action with an explicit device and capability grant; returns JSON. |
| `POST /v1/device-streams` | Read bounded device content with an explicit grant; returns bytes, up to 16 MiB. |

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

Extension actions use the same names and input objects as `cymy extension act`:

```json
{"name":"extension.prepare","input":{"source":"/absolute/path/to/extension.zip"}}
```

For a managed image observation:

```json
{"instanceId":"browser-one","prompt":"find the primary action"}
```

For content interpretation, send the `cymonkey.content/v1alpha1` request body
to `POST /v1/interpretations`:

```json
{"apiVersion":"cymonkey.content/v1alpha1","parts":[{"kind":"text","text":"Meeting starts at 9."}],"prompt":"Summarize"}
```

`cymy operator interpret` reads files on the caller's machine and sends their
bytes to the operator. The content service must have a plugin that advertises
the requested modality. Mixed content requires `multimodal.interpret`.

Interaction calls reuse the display adapter's capability policy, action approvals,
and audit events. Extension and userscript management requires the operator
token and a local source path; extension installation also requires the exact
prepared revision. These manager actions do not yet have separate approval
receipts. Browser installation still follows each browser's native signature
and consent rules.

Device requests use the Cymonkey device contract. For example, the body of
`POST /v1/device-streams` can be:

```json
{"open":{"adapterId":"camera-drive","deviceId":"camera-1","grant":{"capabilities":["drive.read"],"resourceIds":["photos"]}},"action":{"capability":"drive.read","resourceId":"photos","input":{"path":"DCIM/photo.jpg"}}}
```

The single-operation routes require one requested capability per grant and close
the session after the response. Persistent `/v1/device-sessions` routes support
multiple explicit capabilities, WebRTC signaling, recordings, transfers, duplex
channels, expiry, and events. Both surfaces load installed device plugins and
`CYMONKEY_DEVICE_CONFIG`. See [device sessions](device-sessions.md) for the
complete API and configuration. The operator token authorizes grants against
this inventory; a multi-user host must add its own principal policy.
