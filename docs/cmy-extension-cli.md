# Install browser extensions with `cmy`

For all Cymonkey commands, see the [complete `cmy` CLI guide](cmy-cli.md).

`cmy` is the Cymonkey CLI built with a short executable name. It manages
extensions supplied by you; it does not create or own their source. Jangolova
performs the browser-specific installation steps.

## 1. Build the CLI

From the Cymonkey repository root:

```sh
mkdir -p .cache/bin
go build -o .cache/bin/cmy ./src
export PATH="$PWD/.cache/bin:$PATH"
cmy help
```

The repository's usual `cymonkey` executable has the same commands. This
guide uses `cmy` so the examples are shorter.

## 2. Choose a browser and profile

List browsers and profiles found **on the machine running `cmy`**:

```sh
cmy browser targets
```

Each JSON entry has an `id`, `browser`, `name`, and `installMode`. Copy the
`id` for the profile you want. For example, an ID may look like
`firefox:2fcff7241625db28f2574ad6`; use the value your own machine returns.
`manual-stage` means you will complete installation in the browser UI.
`app-handoff` means Safari will ask you to enable the containing app's
extension in Safari Settings.

Discovery covers common local installation and profile locations. To use a
custom browser or profile that is not listed, pass its paths directly instead
of `--target`:

```sh
cmy extension install --browser firefox \
  --browser-bin /absolute/path/to/firefox \
  --profile /absolute/path/to/firefox-profile \
  --source /absolute/path/to/signed-extension.xpi \
  --revision 'sha256:VALUE_FROM_PREPARE'
```

For Chrome, Chromium, or Edge, `--profile` is the **user data directory** and
`--profile-directory` names a profile inside it, such as `Default` or
`Profile 1`. Firefox's `--profile` is the exact Firefox profile directory.
Do not combine `--target` with explicit browser or profile flags. No browser
or profile is selected automatically.

## 3. Prepare the extension

Inspect the exact package or directory you plan to install:

```sh
cmy extension prepare --source /absolute/path/to/extension.zip
```

The JSON response shows the extension's name, version, permissions, and a
`revision` such as `sha256:...`. Copy the **whole revision value** into the
install command. It is a fingerprint of the supplied files, not a Git commit
ID or browser extension ID. If you change the source, run `prepare` again;
installation rejects a source that differs from the supplied revision.

An unpacked directory works too. To make a deterministic ZIP from one:

```sh
cmy extension package --source /absolute/path/to/unpacked-extension \
  --output /absolute/path/to/extension.zip
cmy extension prepare --source /absolute/path/to/extension.zip
```

The ZIP command is useful for review and for Chrome's unpacked install flow.
To produce a signed artifact, use `extension sign` or `extension build-safari`
below. These commands run on the machine running `cmy` with that user's tools
and signing identity. They do not install or publish the extension.

### Optional signing and bundling

For Chrome, Chromium, or Edge, use an installed browser executable to produce
a `.crx` locally. `--target` selects a discovered executable; signing does not
read or modify that target's profile. You can also use `--browser chrome
--browser-bin /absolute/path/to/browser`:

```sh
cmy extension sign --target 'chrome:ID_FROM_TARGETS' \
  --source /absolute/path/to/unpacked-extension \
  --revision 'sha256:VALUE_FROM_PREPARE' \
  --output /absolute/path/to/extension.crx
```

Without `--key`, the browser creates a new private key at
`/absolute/path/to/extension.pem` (mode `0600`). Keep it safe: updates must use
the same key with `--key /absolute/path/to/extension.pem` to retain the
extension ID. The result includes `artifactRevision`, a hash of the CRX, and
`sourceRevision`. On macOS and Windows Chrome, a locally signed CRX does not
grant a general direct-install route; use the original source with `extension
install` or `extension run`, or publish through the Chrome Web Store. [Chrome's
installation rules](https://developer.chrome.com/docs/extensions/how-to/distribute/install-extensions)
still apply.

For Firefox, install `web-ext` on the same machine and provide your Mozilla
add-on API credentials as environment variables. `cmy` does not accept those
secrets as flags. This submits the caller's staged source to Mozilla for
**unlisted** signing and downloads the resulting XPI:

```sh
export WEB_EXT_API_KEY='YOUR_AMO_KEY'
export WEB_EXT_API_SECRET='YOUR_AMO_SECRET'
cmy extension sign --browser firefox \
  --source /absolute/path/to/unpacked-extension \
  --revision 'sha256:VALUE_FROM_PREPARE' \
  --output /absolute/path/to/signed-extension.xpi
```

The result's `artifactRevision` is the revision to use when installing that
XPI. Manifest V3 extensions need a Firefox extension ID in their manifest;
Mozilla may also require information or review for a first submission. The
signing service and account belong to the caller.

For Safari on macOS, generate the Xcode project as shown below, then build a
signed app using your Xcode team. Supply the generated `.xcodeproj` path and
its app scheme:

```sh
cmy extension build-safari \
  --project /absolute/path/to/project/MyExtension.xcodeproj \
  --scheme MyExtension --team-id YOUR_APPLE_TEAM_ID \
  --output /absolute/path/to/MyExtension.app
```

The build verifies the app signature and embedded Safari WebExtension. Its
`artifactRevision` starts with `cdhash:` and is the revision for `extension
install`. A local signed app can be used with Safari's enablement flow;
distribution outside the Mac App Store may require Developer ID signing and
Apple notarization. That distribution step is not performed by this command.

## 4. Install in the selected browser

### Firefox

Use a **Mozilla-signed XPI** from the optional signing step above or another
source, and close the selected Firefox profile before starting. Prepare the
XPI itself, then install it into the discovered target:

```sh
cmy extension prepare --source /absolute/path/to/signed-extension.xpi
cmy extension install --target 'firefox:ID_FROM_TARGETS' \
  --source /absolute/path/to/signed-extension.xpi \
  --revision 'sha256:VALUE_FROM_PREPARE'
```

Firefox checks the XPI signature. Jangolova restarts that profile and prints
`installed` only after it confirms the extension remained active. Leave the
command running while using the opened browser; it ends when the browser
closes or you stop it.

### Chrome, Chromium, or Edge

Choose a stable, **new** staging directory outside the source. The browser
will continue to load the unpacked extension from that directory:

```sh
cmy extension install --target 'chromium:ID_FROM_TARGETS' \
  --source /absolute/path/to/extension.zip \
  --revision 'sha256:VALUE_FROM_PREPARE' \
  --destination /absolute/stable/path/to/staged-extension
```

For a target with `installMode: "native"`, `cmy` opens the browser's extension
page. In that browser, enable Developer mode and choose **Load unpacked**, then
select the staged directory shown in the JSON output. Jangolova restarts the
browser and prints `installed` only if the extension is still enabled.

Chrome's default user data directory is listed as `manual-stage`: `cmy`
prepares the files and prints `awaiting-browser-action`, but cannot verify the
installation there. Open the selected Chrome profile and complete **Load
unpacked** yourself. A custom Chrome user data directory can use the verified
native route; point to it explicitly:

```sh
cmy extension install --browser chrome \
  --browser-bin /absolute/path/to/chrome \
  --profile /absolute/path/to/custom-chrome-user-data \
  --profile-directory Default \
  --source /absolute/path/to/extension.zip \
  --revision 'sha256:VALUE_FROM_PREPARE' \
  --destination /absolute/stable/path/to/staged-extension
```

Close the selected profile in other browser processes before starting the
verified route.

### Safari on macOS

Safari needs a macOS app containing the WebExtension. To create a caller-owned
Xcode project from your WebExtension files:

```sh
cmy extension prepare --source /absolute/path/to/extension.zip
cmy extension package-safari --source /absolute/path/to/extension.zip \
  --revision 'sha256:VALUE_FROM_ZIP_PREPARE' \
  --output /absolute/path/to/new-project-location \
  --bundle-id com.example.myextension --app-name 'My Extension'
```

Use `extension build-safari` above to build and sign that project as your own
app. Then prepare the **signed app**; this produces a different revision,
beginning with `cdhash:`:

```sh
cmy extension prepare --source /absolute/path/to/MyExtension.app
cmy extension install --target 'safari:ID_FROM_TARGETS' \
  --source /absolute/path/to/MyExtension.app \
  --revision 'cdhash:VALUE_FROM_APP_PREPARE'
```

`cmy` opens the containing app and prints `awaiting-browser-action`. Enable
the extension for the intended Safari profile and websites in **Safari >
Settings > Extensions**. Safari controls that profile choice; `--profile` is
not accepted for Safari.

## Read the result

| Status | Meaning |
| --- | --- |
| `awaiting-browser-action` | Complete the action named in `nextAction` in the browser. |
| `installed` | Jangolova verified a persistent installation after restart. |
| `activated` | The extension is active only for the controlled browser session started by `extension run`. |
| `packaged` | Safari project files were generated; build and sign the app before installing. |
| `signed` | A CRX, XPI, or Safari app artifact was produced. This does not mean it is installed. |

The `install` command may print multiple JSON lines as its state changes.
`source`, `profile`, and browser-assigned `id` in those lines help confirm
which browser and extension were affected. Errors exit with a nonzero code.

## Calling from another local tool

Tools can use the same CLI without parsing prose:

```sh
cmy extension act --name extension.targets --input '{}'
cmy extension act --name extension.prepare \
  --input '{"source":"/absolute/path/to/signed-extension.xpi"}'
cmy extension act --name extension.install --input '{
  "source":"/absolute/path/to/signed-extension.xpi",
  "revision":"sha256:VALUE_FROM_PREPARE",
  "target":{"id":"firefox:ID_FROM_TARGETS"}
}'
```

Read the `revision` returned by `extension.prepare` and pass it unchanged with
the same `source` to `extension.install`. The `install` action streams JSON
lines until the selected browser closes. This is a local CLI interface; a
remote authenticated tool bridge is not yet available.

For adapter behavior and platform limits, see the
[browser extension manager reference](browser-extension-installation.md).
