# Liveframe

`pkg/liveframe` is Cymonkey's optional connector for a live, agent-authored web
workspace. Its first backend is the installed
[Canvas CLI](https://github.com/steipete/canvas) (`v0.1.0` tested). Canvas serves
files, owns one Chromium tab, and exposes a local CDP endpoint. Liveframe
discovers that session and produces a target for Cymonkey's existing
`web-presentation` adapter. It does **not** vendor Canvas, launch a browser,
stop one, or take over an unrelated tab.

## Start a workspace

Install Canvas separately and select an absolute workspace directory. The
caller can create the starter page without overwriting an existing page:

```go
workspace := "/absolute/path/to/my-liveframe"
_, err := liveframe.InstallStarter(workspace)
if err != nil { /* handle the error */ }
```

Then the owner starts Canvas, for example:

```sh
canvas start --dir /absolute/path/to/my-liveframe
canvas status --json
```

The starter exposes `window.cymonkey.display` with the versioned display
bridge. Cymonkey can use `presentation.write`, `presentation.execute`,
`presentation.describe`, and `presentation.capture`. `presentation.write`
accepts `{html, css?, js?, expectedStateRevision}`. Read the revision with
`describe` before writing. Canvas automatically reloads the page when the
workspace file changes; after reload, the in-page revision starts at `0`.

## Attach through Cymonkey

```go
connector := liveframe.Connector{} // or set CanvasPath / StateDir
session, err := connector.Discover(ctx)
if err != nil { /* Canvas is absent, stopped, or not a local live session */ }

request, err := session.CymonkeyRequest("liveframe-demo")
if err != nil { /* handle the error */ }
// JSON-encode request and send it to Cymonkey's normal display connect API.
```

The request selects `web-presentation`, uses the Canvas page as `source`, and
passes its loopback CDP URL as a caller-owned target. It restricts the source
and assets to the Canvas origin and requires an approval receipt for
`presentation.write`, `presentation.execute`, and `presentation.capture`.
These actions also remain subject to the presentation adapter's authorization
and audit policy. This descriptor
does not itself issue approval. For library callers, `session.Target()` and
`session.PresentationSpec()` return the corresponding Jangolova SDK values.

After an ordinary Canvas file reload, the attached presentation adapter can
continue using the same page. After a Canvas restart or navigation, call
`connector.Refresh(ctx, session)`: when `changed` is true, detach the old
presentation instance and connect again with the returned session. Detach
must not call `canvas stop`; the target owner remains responsible for lifecycle.

`Connector.StateDir` selects a caller-owned `CANVAS_STATE_DIR` without changing
the global Canvas session. Liveframe rejects non-loopback HTTP/CDP addresses,
unavailable browser processes, and missing workspace directories.

## Verification

```sh
GOWORK=off go test ./pkg/liveframe ./src/internal/engineprovider ./src/adapters/webpresentation

# Optional macOS live test; starts/stops only a temporary Canvas session.
GOWORK=off LIVEFRAME_CHROME_BIN='/Applications/Chrome.app/Contents/MacOS/Google Chrome' \
  go test -tags=liveframe_live ./src/adapters/webpresentation \
  -run '^TestLiveframeCanvasRoundTrip$' -v -count=1
```

The live test checks bridge conformance, semantic write, PNG capture, Canvas
file auto-reload, and that detaching leaves Canvas running. It uses a short
private state path because macOS limits Unix socket path length. The current
root `go.work` references unrelated provider modules that are absent in some
checkouts; `GOWORK=off` runs these focused tests through the root module.

Liveframe is not an existing-tab augmentation backend. For an existing site,
use Cymonkey's browser targets/userscripts instead; Canvas currently controls
its own tab.
