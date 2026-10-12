# Install browser extensions with `cymy`

For all Cymonkey commands, see the [complete `cymy` CLI guide](cymy-cli.md).

`cymy` is the Cymonkey CLI. It manages
extensions supplied by you; it does not create or own their source. Trusted
ext browser adapters perform the browser-specific steps, so install and trust
the adapter for your browser first:

```sh
cymy adapters install --source /path/to/ext/adapters/chrome
cymy adapters trust chrome
```

`--browser` is the adapter name (`chrome`, `edge`, `firefox`, `safari`, ...).
Session-only `--headless` is no longer a flag; the adapter decides how a
session loads.

## 1. Build the CLI

From the Cymonkey repository root:

```sh
mkdir -p .cache/bin
go build -o .cache/bin/cymy ./src
export PATH="$PWD/.cache/bin:$PATH"
cymy help
```

The existing `cymonkey` executable has the same commands for deployment
compatibility.

## 2. Choose a browser and profile

List browsers and profiles found **on the machine running `cymy`**:

```sh
cymy browser targets
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
cymy extension install --browser firefox \
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
cymy extension prepare --source /absolute/path/to/extension.zip
```

The JSON response shows the extension's name, version, permissions, and a
`revision` such as `sha256:...`. Copy the **whole revision value** into the
install command. It is a fingerprint of the supplied files, not a Git commit
ID or browser extension ID. If you change the source, run `prepare` again;
installation rejects a source that differs from the supplied revision.

An unpacked directory works too. To make a deterministic ZIP from one:

```sh
cymy extension package --source /absolute/path/to/unpacked-extension \
  --output /absolute/path/to/extension.zip
cymy extension prepare --source /absolute/path/to/extension.zip
```

The ZIP command is useful for review and for Chrome's unpacked install flow.
To produce a signed artifact, use `extension sign` or `extension build-safari`
below. These commands run on the machine running `cymy` with that user's tools
and signing identity. They do not install or publish the extension.

### Optional signing and bundling

For Chrome, Chromium, or Edge, use an installed browser executable to produce
a `.crx` locally. `--target` selects a discovered executable; signing does not
read or modify that target's profile. You can also use `--browser chrome
--browser-bin /absolute/path/to/browser`:

```sh
cymy extension sign --target 'chrome:ID_FROM_TARGETS' \
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
add-on API credentials as environment variables. `cymy` does not accept those
secrets as flags. This submits the caller's staged source to Mozilla for
**unlisted** signing and downloads the resulting XPI:

```sh
export WEB_EXT_API_KEY='YOUR_AMO_KEY'
export WEB_EXT_API_SECRET='YOUR_AMO_SECRET'
cymy extension sign --browser firefox \
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
cymy extension build-safari \
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
cymy extension prepare --source /absolute/path/to/signed-extension.xpi
cymy extension install --target 'firefox:ID_FROM_TARGETS' \
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
cymy extension install --target 'chromium:ID_FROM_TARGETS' \
  --source /absolute/path/to/extension.zip \
  --revision 'sha256:VALUE_FROM_PREPARE' \
  --destination /absolute/stable/path/to/staged-extension
```

For a target with `installMode: "native"`, `cymy` opens the browser's extension
page. In that browser, enable Developer mode and choose **Load unpacked**, then
select the staged directory shown in the JSON output. Jangolova restarts the
browser and prints `installed` only if the extension is still enabled.

Chrome's default user data directory is listed as `manual-stage`: `cymy`
prepares the files and prints `awaiting-browser-action`, but cannot verify the
installation there. Open the selected Chrome profile and complete **Load
unpacked** yourself. A custom Chrome user data directory can use the verified
native route; point to it explicitly:

```sh
cymy extension install --browser chrome \
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
cymy extension prepare --source /absolute/path/to/extension.zip
cymy extension package-safari --source /absolute/path/to/extension.zip \
  --revision 'sha256:VALUE_FROM_ZIP_PREPARE' \
  --output /absolute/path/to/new-project-location \
  --bundle-id com.example.myextension --app-name 'My Extension'
```

Use `extension build-safari` above to build and sign that project as your own
app. Then prepare the **signed app**; this produces a different revision,
beginning with `cdhash:`:

```sh
cymy extension prepare --source /absolute/path/to/MyExtension.app
cymy extension install --target 'safari:ID_FROM_TARGETS' \
  --source /absolute/path/to/MyExtension.app \
  --revision 'cdhash:VALUE_FROM_APP_PREPARE'
```

`cymy` opens the containing app and prints `awaiting-browser-action`. Enable
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
| `awaiting-browser-confirmation` | A Windows or macOS external store request was registered; the browser still needs to read and confirm it. |
| `requested` | A Linux external store request was written; inspect the browser after restart. |
| `request-removed` | The matching external request was removed; this does not prove the extension was uninstalled. |
| `policy-updated` | An administrator policy was changed; inspect the browser's policy and extension pages to verify its effect. |

The `install` command may print multiple JSON lines as its state changes.
`source`, `profile`, and browser-assigned `id` in those lines help confirm
which browser and extension were affected. Errors exit with a nonzero code.

## Request a published extension from a store

For Chrome Web Store or Microsoft Edge Add-ons extensions, use the published
32-character extension ID. This route does not require an extension package:

```sh
cymy extension install-store --browser chrome --id abcdefghijklmnopabcdefghijklmnop
cymy extension install-store --browser edge --id abcdefghijklmnopabcdefghijklmnop
```

Edge can also request an extension published in the Chrome Web Store:

```sh
cymy extension install-store --browser edge --store chrome \
  --id abcdefghijklmnopabcdefghijklmnop
```

Use `--target ID_FROM_BROWSER_TARGETS` instead of `--browser` when convenient.
External requests apply to the browser installation, not a selected Chromium
profile. On macOS the default JSON directory belongs to the current user; on
Linux Edge uses the current user's directory. Chrome Linux uses
`/opt/google/chrome/extensions` and normally requires administrator access.
Set `--external-dir` to another documented path, such as
`/usr/share/google-chrome/extensions`, when appropriate. On Windows the
command invokes PowerShell to write the documented machine registry key and
needs administrator rights; `--external-dir` is not used.

To withdraw a matching request:

```sh
cymy extension remove-store --browser edge --id abcdefghijklmnopabcdefghijklmnop
```

Use the same `--store` and `--external-dir` values used when creating it.
The structured tool equivalents are `extension.install-store` and
`extension.remove-store`, for example:

```json
{"target":{"browser":"edge"},"store":"chrome","id":"abcdefghijklmnopabcdefghijklmnop"}
```

Windows and macOS require the browser's native confirmation for external
installs. Linux reads the request without that prompt. In every case, check
the browser's extensions page before reporting the extension as installed.
These requests are for published store extensions; a locally signed CRX still
needs a route allowed by that browser and operating system.

## Administrator policy management

On an administrator-managed Windows machine, `cymy` can update Chrome or Edge
force-install and block policies through PowerShell:

```sh
cymy extension policy --browser edge --id abcdefghijklmnopabcdefghijklmnop --mode force
cymy extension policy --browser edge --id abcdefghijklmnopabcdefghijklmnop --mode unforce
cymy extension policy --browser edge --id abcdefghijklmnopabcdefghijklmnop --mode block
cymy extension policy --browser edge --id abcdefghijklmnopabcdefghijklmnop --mode unblock
```

Use `--store chrome` when force-installing a Chrome Web Store item into Edge.
On Linux this command supports managed Google Chrome with an administrator-
owned JSON file at `/etc/opt/chrome/policies/managed/jangolova-extensions.json`.
It refuses to add a policy key already defined in another JSON file, because
Chrome does not define which duplicate value wins. Mac policies require an
administrator-deployed configuration profile; this command does not write one.
Edge's force-install and block policies are not documented for Linux, so the
Linux policy adapter is Chrome-only.

The structured equivalent is `extension.policy` with a `mode` field. These
policies can install or disable an extension without a browser prompt and can
prevent the user from removing it. Use them only for a browser installation
you administer. A `policy-updated` response means the policy value changed;
verify its effect on `chrome://policy` or `edge://policy` and the browser's
extensions page. Force-install takes precedence over a blocklist entry for
the same ID.

## Calling from another local tool

Tools can use the same CLI without parsing prose:

```sh
cymy extension act --name extension.targets --input '{}'
cymy extension act --name extension.prepare \
  --input '{"source":"/absolute/path/to/signed-extension.xpi"}'
cymy extension act --name extension.install --input '{
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
