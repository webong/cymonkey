# Browser extension manager

Cymonkey manages browser extensions supplied by users and other tools, just as
it manages userscripts. It does not author, modify, or bundle those extensions
into its own browser extension. Jangolova owns the browser-specific inspection,
packaging, staging, and installation adapters in
`lib/jangolova/browserextension`; Cymonkey exposes their semantic workflow.
The caller retains ownership of the submitted extension and its source.

## Local package workflow

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

`prepare` (also available as `inspect`) accepts a ZIP or unpacked directory
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
against Chrome's **default** user data directory. This command rejects that
directory and needs a custom user data directory. For an existing default
Chrome profile, use `extension stage` and complete Load unpacked in Chrome;
the current local CLI cannot verify persistence there. Chrome documents the
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

The supported one-shot actions are `extension.capabilities`,
`extension.prepare`, `extension.package`, `extension.stage`, and
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
first JSON line to obtain a session-only activation. On unsupported browsers,
`extension.install` returns an error and never reports `installed`.

After the browser completes installation, Cymonkey's privileged WebExtension
offers `extension.list` and `extension.describe` through `capabilities`/`act`.
These read the browser's native management API and return source-free installed
state, including the browser-assigned ID, version, enabled flag, install type,
and declared permissions. They are advertised only where that API exists.

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
update URL; that requires a separate administrator-owned adapter. Firefox
requires a signed XPI for normal distribution, and Safari distributes its
WebExtension inside an app. Native install adapters for Firefox, Safari, and
Windows are not implemented yet. No adapter may claim success before the
browser reports the extension installed.

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
