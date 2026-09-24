# Browser extension manager

For step-by-step terminal commands, see [Install browser extensions with `cmy`](cmy-extension-cli.md).

Cymonkey manages browser extensions supplied by users and other tools, just as
it manages userscripts. It does not author, modify, or bundle those extensions
into its own browser extension. Jangolova owns the browser-specific inspection,
packaging, staging, and installation adapters in
`lib/jangolova/browserextension`; Cymonkey exposes their semantic workflow.
The caller retains ownership of the submitted extension and its source.

## Build an extension manager with Cymonkey

Cymonkey also provides the reusable
[`@cymonkey/extension-manager` library](../pkg/extension-manager/README.md).
An extension can import it, pass its own native `management` API, and implement
its own list, detail, enable/disable, uninstall, and change-event UI. That
extension is self-owned. The library has
an optional installation-host interface so the integrating app can connect
its own authenticated Cymonkey/Jangolova host for package preparation and
browser-specific installation. The extension owns its UI and permissions;
Cymonkey owns the shared interface and host workflow.

## Local package workflow

Discover browsers and existing profiles on the machine running Cymonkey:

```sh
cymonkey browser targets
cymonkey extension act --name extension.targets --input '{}'
```

The result contains a target ID, browser, executable path, profile path,
and install mode. Discovery reads known local application and profile locations
without launching a browser. It does not find every custom installation. A
caller may always specify `--browser`, `--browser-bin`, and `--profile` paths
directly. IDs remain stable while those paths stay the same. No browser or
profile is selected automatically.

### Get the revision before installing

Run `prepare` on the **same source path** you will pass to `install`:

```sh
cymonkey extension prepare --source /path/to/extension.zip
```

The response includes a `revision` field, for example:

```json
{
  "name": "Example Extension",
  "version": "1.0",
  "revision": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
```

Copy the complete `revision` value into `--revision` (or the `revision` field
of `extension.install`). It is generated from the extension's file names and
contents; it is **not** a Git commit ID or a browser extension ID. If the
source changes after `prepare`, run `prepare` again and review the new revision.
Installation rejects a source that no longer matches the supplied revision.

For a local tool, call `extension.prepare` and pass the returned `revision` to
`extension.install` with the same `source`:

```sh
cymonkey extension act --name extension.prepare \
  --input '{"source":"/path/to/extension.zip"}'
```

A signed Safari `.app` uses the same `prepare` command, but its revision starts
with `cdhash:` and comes from the app's code signature. Prepare the **built,
signed app** before calling `extension.install`; the earlier `sha256:` revision
of the WebExtension ZIP is used only when creating the Safari Xcode project.

For a discovered target, pass its ID instead of browser and profile flags:

```sh
cymonkey extension install --target 'firefox:TARGET_ID' \
  --source /path/to/signed-extension.xpi \
  --revision 'sha256:REVISION_FROM_PREPARE'
```

Structured callers pass `{"target":{"id":"firefox:TARGET_ID"}}` to
`extension.install`. The target is resolved again when the action runs; a
missing or stale ID fails rather than choosing another profile. Target IDs
cannot be combined with explicit browser or profile fields.

```sh
cymonkey extension capabilities --browser chrome
cymonkey extension prepare --source /path/to/extension.zip
cymonkey extension package --source /path/to/unpacked-extension --output /path/to/extension.zip
cymonkey extension install --browser chrome --browser-bin /absolute/path/to/browser \
  --profile /absolute/path/to/browser-user-data --profile-directory Default \
  --source /path/to/extension.zip --revision 'sha256:REVISION_FROM_PREPARE' \
  --destination /stable/path/to/staged-extension
cymonkey extension run --browser chrome --browser-bin /absolute/path/to/browser --headless=false \
  --profile /absolute/path/to/browser-user-data --profile-directory Default \
  --source /path/to/extension.zip \
  --revision 'sha256:REVISION_FROM_PREPARE' --destination /path/to/staged-extension
```

`prepare` (also available as `inspect`) accepts a ZIP, Firefox XPI, or unpacked directory
containing `manifest.json`. A
single wrapping directory in a ZIP is allowed. It returns the extension name,
version, declared permissions, optional Chromium ID from the public manifest
key, file count, byte count, and a content revision. It never returns source.

`package` creates a deterministic ZIP. It does **not** create a signed Chromium
CRX, signed Firefox XPI, Safari app, or Web Store listing. Keep a Chromium
signing key outside this ZIP; a ZIP without a manifest public key has no stable
Chromium extension ID.

`install` supports a persistent unpacked extension in a user-selected Chrome,
Chromium, or Edge profile on macOS or Linux. `--profile` is the browser's **user
data directory**, and `--profile-directory` selects the profile within it (for
example `Default` or `Profile 1`). Close that profile in other browser processes
before starting this command. Use a stable `--destination`: the browser loads
the extension from that directory after installation.

For Chrome 136 and newer, the pipe used to verify installation is not honored
against Chrome's **default** user data directory. For an existing default
Chrome profile, `extension install` stages the files and returns
`awaiting-browser-action` with the selected profile and native Load unpacked
steps. The CLI cannot verify persistence there. For a verified `installed`
result, select a custom user data directory. Chrome documents the
[default-directory debugging restriction](https://developer.chrome.com/blog/remote-debugging-port).

The command emits `awaiting-browser-action` after opening the selected
browser's extension page. In that browser, enable Developer mode, choose **Load
unpacked**, and select the returned staged directory. Jangolova observes the
exact directory and browser-assigned ID, restarts the browser once, and reports
`installed` only if the same extension is enabled after restart and the staged
files still match the prepared revision. The restarted browser remains open
until the user closes it or stops the command. The staged directory must remain
in place. This route uses the browser's native installation action; it does not
silently bypass it.

`run` validates and stages the package, launches the selected Chrome,
Chromium, or Edge executable with a debugging pipe against the caller-selected
profile, and uses the browser's `Extensions.loadUnpacked` command. It verifies
the returned ID in the browser's extension inventory, prints `activated`,
and keeps the browser process running. The staged directory must remain
available. The selected profile cannot already be open in another browser
process. The result includes the browser-assigned extension ID and selected
profile path. `--revision` binds this session to the package prepared for
review; staging verifies the copied files against that revision before the
browser loads it.

This DevTools route does **not** create a persistent native installation. A
live test against Chrome showed the extension loaded during the controlled
session but absent after the profile was reopened. Cymonkey therefore never
reports `installed` for this route. The caller must keep the `extension run`
process active and launch the browser through Cymonkey again for a later
session.

These adapters must launch the selected browser themselves. They cannot attach the
pipe-only loading command to an ordinary browser process that is already
running. Callers choose the executable and profile; Jangolova does not assume
their default browser or profile. `extension.capabilities` distinguishes
session loading from guided persistent installation for the selected browser. An
unsupported browser gets a clear unsupported result.

`extension stage` is a manual fallback for Chrome, Chromium, and Edge. It
returns `awaiting-browser-action` with the staged path. The caller can then
complete that browser's native installation flow. An unsupported target is
never reported as installed.

The file adapter rejects path traversal, duplicate entries, symlinks, special
files, encrypted ZIPs, oversized files, oversized archives, and overwriting an
existing output or staged directory.

## Calling the manager from another local tool

The `extension act` entry point accepts a named action and JSON input and
returns JSON. The tool supplies its extension package path; Cymonkey routes
the request to Jangolova's browser adapter. For example:

```sh
cymonkey extension act --name extension.prepare \
  --input '{"source":"/path/to/extension.zip"}'
cymonkey extension act --name extension.capabilities \
  --input '{"target":{"browser":"chrome"}}'
```

The supported one-shot actions are `extension.targets`, `extension.capabilities`,
`extension.prepare`, `extension.package`, `extension.package-safari`, `extension.stage`, and
`extension.install-store`. `extension.install` is a long-running action for the
guided native route. A caller can spawn it and read JSON lines for
`awaiting-browser-action` and, after restart verification, `installed`:

```sh
cymonkey extension act --name extension.install --input '{
  "source":"/path/to/extension.zip",
  "destination":"/stable/path/to/staged-extension",
  "revision":"sha256:REVISION_FROM_PREPARE",
  "target":{
    "browser":"chrome",
    "executablePath":"/absolute/path/to/browser",
    "userDataDir":"/absolute/path/to/browser-user-data",
    "profileDirectory":"Default"
  }
}'
```

A caller can also spawn the long-running `extension run` command and read its
first JSON line to obtain a session-only activation. Firefox uses the same
long-running `extension.install` action with `target.browser` set to `firefox`,
`target.executablePath`, and `target.profilePath` for the selected profile.
Safari uses `extension.package-safari` and `extension.install` with
`target.browser` set to `safari`, or its discovered Safari target ID.

After installation, a caller-owned WebExtension can use
[`@cymonkey/extension-manager`](../pkg/extension-manager/README.md) to read
the browser's native management API. Its `list` and `describe` methods return
source-free installed state, including the browser-assigned ID, version,
enabled flag, install type, and declared permissions, when that API exists.

## Firefox

Firefox accepts a signed XPI for permanent installation. The caller owns the
extension and obtains Mozilla signing (listed or unlisted); Cymonkey does not
hold signing credentials. The XPI must remain intact because re-zipping it
would invalidate the signature. Firefox verifies the signature itself.

```sh
cymonkey extension prepare --source /absolute/path/to/signed-extension.xpi
cymonkey extension install --browser firefox \
  --browser-bin /Applications/Firefox.app/Contents/MacOS/firefox \
  --profile /absolute/path/to/firefox-profile \
  --source /absolute/path/to/signed-extension.xpi \
  --revision 'sha256:REVISION_FROM_PREPARE'
```

Close the selected profile first. Jangolova launches Firefox with a local
WebDriver BiDi endpoint, calls `webExtension.install` with `moz:permanent`,
checks Firefox's native extension inventory where supported, restarts Firefox,
and reports `installed` only when the same extension is active and permanent
after restart. Older Firefox releases use the profile metadata and the copied
XPI content for that restart check.
An unsigned XPI is rejected by Firefox. The browser stays open until the
command ends.

## Safari on macOS

Safari distributes a WebExtension inside a macOS app. Jangolova can convert
caller-owned WebExtension files into a caller-owned Xcode project, using the
Safari packager available in Xcode (or its earlier converter name):

```sh
cymonkey extension prepare --source /absolute/path/to/extension.zip
cymonkey extension package-safari --source /absolute/path/to/extension.zip \
  --revision 'sha256:REVISION_FROM_PREPARE' \
  --output /absolute/path/to/new-project-location \
  --bundle-id com.example.myextension --app-name 'My Extension'
```

The caller builds and signs the generated app with its own Apple developer
identity. For a built app containing a Safari WebExtension:

```sh
cymonkey extension prepare --source /absolute/path/to/MyExtension.app
cymonkey extension install --browser safari \
  --source /absolute/path/to/MyExtension.app \
  --revision 'cdhash:REVISION_FROM_PREPARE'
```

Jangolova checks the app signature and embedded Safari extension identity,
then opens the containing app. Safari requires the user to enable the extension
in Safari Settings and allow it for the intended profile and websites. This
route returns `awaiting-browser-action`; the local CLI does not claim Safari is
enabled based only on app launch. `--profile` is rejected for Safari because
that choice belongs to Safari's settings. Apple's temporary developer extension flow
expires when Safari quits or after 24 hours.

## Published Chrome Web Store extension on macOS

For a published Chrome Web Store extension, the host can request external
installation for a particular user by writing Chrome's documented JSON file:

```sh
cymonkey extension install-store \
  --id abcdefghijklmnopabcdefghijklmnop \
  --external-dir "$HOME/Library/Application Support/Google/Chrome/External Extensions"
```

The result is `awaiting-browser-confirmation`. Chrome reads this preference on
launch and requires the user to enable the extension. The command refuses an
existing file and symlinks in the chosen profile path.

This route accepts a **Web Store ID only**. Chrome does not allow a local CRX
or self-hosted ZIP to be installed through a personal macOS external preference
file. A managed Chrome deployment can use enterprise policy and a hosted
update URL; that requires a separate administrator-owned adapter. Windows
Chromium installation remains unimplemented. Safari app launch does not prove
the extension is enabled, so it never reports `installed` by itself.

## Ownership

- Caller: supplies and owns the extension source or package.
- Jangolova: source validation, packaging, staging, native install route, and
  browser-specific status verification.
- Cymonkey: manager entry point, public capability names, caller authorization,
  approvals, audit, and cross-runtime lifecycle events.
- Browser or administrator: final installation, browser permission consent,
  and enterprise deployment policy.

The CLI and its structured `act` form are the initial local host surfaces that
other local tools can invoke.
The authenticated Cymonkey control plane still needs a host bridge before
remote callers can submit packages. That bridge must bind installation to an
explicit browser profile and an immutable reviewed revision, and require the
same revision on the final request. The existing WebExtension's `management`
API can describe installed extensions, but it cannot install an arbitrary ZIP.
