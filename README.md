# Cymonkey

Cymonkey is the operating and extension layer for agent systems that need to work
with real interfaces. It gives a way to use and extend
its submodules through structured capabilities, policy, and caller-owned
targets.

It supports both sides of interface work: operating an existing experience and
presenting a new dynamic one.

## Capabilities

- Connect to browsers through CDP, WebDriver BiDi, WebDriver Classic, Safari
  MCP, WebExtension, and native integrations.
- Supply libraries that an extension or browser host can use to mount reviewed
  browser packages into an approved tab.
- Expose typed, policy-governed actions for navigation, interaction, display,
  presentation, and runtime-specific scene controls.
- Operate explicit Three.js, Godot, Unity, Unreal, and Blender resources through their
  runtime libraries.
- Request image observations or interpret text, image, audio, and video through
  the public content contract. The selected plugin determines its supported tasks.
- Prepare typed, host-approved input and output bindings through the public device contract.
- Reuse native capture/input/recording, device readiness, storage transfer, and
  endpoint health SDKs.
- Compose callable tools, one explicit model turn, and caller-selected MCP
  attachments through the Console SDK. See [model and tool composition](docs/sdk-model-tools.md).
- Install optional display, content, and device plugins without rebuilding Cymonkey.
- Preserve target ownership: a connection may detach, but it never quits a
  browser, terminates an application, or destroys a scene.

## Architecture

```text
agent or application
  → Cymonkey
    → display contract
      → browser, desktop surface, Three.js, Godot, Unity, Unreal, or Blender
    → content contract
      → local or cloud models for text, image, and sound
    → device contract
      → registered keyboard, drive, capture, audio, and future device plugins
```

Cymy and the operator API expose only Cymonkey contracts: `display`, `content`,
and `device`. Plugin manifests declare `cymonkey.display`, `cymonkey.content`,
or `cymonkey.device`. Configuration, HTTP payloads, process methods, and command
output use the same public vocabulary.

See the [Cymonkey plugin contract](docs/cymonkey-plugins.md) for the extension
boundary.

## Quick start

Build the executables and start Cymonkey:

The example content component expects the `local-ultralytics` plugin and a
`yolo-sam@1.0.0` model to be installed first. Follow the
[content configuration guide](scripts/deploy/content/README.md) once per host.

```bash
mkdir -p .cache/bin
go build -o .cache/bin/cymy ./src

export CYMONKEY_DISPLAY_TOKEN="replace-with-a-random-secret"
export CYMONKEY_CONTENT_TOKEN="replace-with-another-random-secret"
PATH="$PWD/.cache/bin:$PATH" cymy run \
  --config scripts/deploy/cymonkey/cymonkey.example.yaml
```

The host manifest starts the display and content services.
After a target has been connected, request a visual observation with:

```bash
PATH="$PWD/.cache/bin:$PATH" cymy observe \
  --config scripts/deploy/cymonkey/cymonkey.example.yaml \
  --instance browser-1 \
  --prompt "find the primary action"
```

## Operating model

Cymonkey is not an agent or model gateway. Your agent, IDE, or application owns
planning and decisions. Cymonkey enforces the boundary between that decision
maker and the target runtime through explicit capabilities, policy, approvals,
audit events, reviewed packages, and caller-supplied target connections.

## Documentation

- [Console tool composition library](pkg/console/README.md)
- [Display interaction](docs/engine-provider.md)
- [Content observation and interpretation](docs/content.md)
- [Device input and output](docs/device-sessions.md)
- [Architecture](docs/architecture.md)
- [Browser packages](docs/browser-augmentation-packages.md)
- [Browser extension manager](docs/browser-extension-installation.md)
- [Browser helper library for extensions, userscripts, and browser APIs](pkg/browser-helper/README.md)
- [macOS native and WebKit helper libraries](lib/macos-helper/README.md)
- [iOS web-view helper library](lib/ios-helper/README.md)
- [Android web-view helper library](lib/android-helper/README.md)
- [Windows native and WebView2 helper libraries](lib/windows-helper/README.md)
- [Liveframe Canvas workspace connector](pkg/liveframe/README.md)
- [Use the `cymy` CLI](docs/cymy-cli.md)
- [Persistent operator API](docs/operator-api.md)
- [Install extensions with the `cymy` CLI](docs/cymy-extension-cli.md)
- [Manage userscripts without an extension](docs/userscripts.md#extension-free-local-manager)
- [Target connection security](docs/target-connection-security.md)
- [Roadmap](docs/roadmap.md)
- [Display render modules](docs/display-render-modules.md)

## Development

```bash
go test ./...
npm run test:cymonkey
npm run test:browser-helper
npm run test:threejs-cymonkey
```
