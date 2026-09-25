# Cymonkey

Cymonkey is the operating and extension layer for AI systems that need to work
with real interfaces. It gives agents and applications a way to use and extend
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
- Request image inference through Blockade, including local YOLO, SAM, ONNX,
  and browser-local WebLLM paths. Blockade also defines a provider interface
  for separately supplied cloud inference adapters.
- Prepare typed, host-approved input and output device bindings through Board.
- Preserve target ownership: a connection may detach, but it never quits a
  browser, terminates an application, or destroys a scene.

## Architecture

```text
agent or application
  → Cymonkey
    → Jangolova display interfaces
      → browser, desktop surface, Three.js, Godot, Unity, Unreal, or Blender
    → Blockade inference interfaces
      → local or cloud backends for image and, as the contract grows, sound
    → Board input and output device interfaces
      → registered keyboard, drive, and future device providers
```

Jangolova provides interfaces for operating and presenting on displays.
Blockade provides inference interfaces for image and sound across local and
cloud backends. Board defines the provider and grant contract for device I/O;
native providers and a Cymonkey binding are still to be implemented. The
current Blockade request contract is image-based; sound inference is part of
its intended scope and is not implemented yet. Cymonkey provides the boundary
through which callers extend and coordinate these libraries.

All three libraries are independently usable. Cymonkey can compose their public
Go modules and supply the host services that its workflows need; none
imports Cymonkey or requires the Cymonkey executable.

## Quick start

Build the executables and start Cymonkey:

```bash
mkdir -p .cache/bin
go build -o .cache/bin/cymonkey ./src

export CYMONKEY_JANGOLOVA_TOKEN="replace-with-a-random-secret"
PATH="$PWD/.cache/bin:$PATH" cymonkey run \
  --config infra/deploy/cymonkey/cymonkey.example.yaml
```

The host manifest starts the display-interface provider and inference service.
After a target has been connected, request a visual observation with:

```bash
PATH="$PWD/.cache/bin:$PATH" cymonkey observe \
  --config infra/deploy/cymonkey/cymonkey.example.yaml \
  --instance browser-1 \
  --prompt "find the primary action"
```

## Operating model

Cymonkey is not an agent or model gateway. Your agent, IDE, or application owns
planning and decisions. Cymonkey enforces the boundary between that decision
maker and the target runtime through explicit capabilities, policy, approvals,
audit events, reviewed packages, and caller-supplied target connections.

## Documentation

- [Jangolova](lib/jangolova/README.md)
- [Blockade](lib/blockade/README.md)
- [Board](lib/board/README.md)
- [Architecture](docs/architecture.md)
- [Browser packages](docs/browser-augmentation-packages.md)
- [Browser extension manager](docs/browser-extension-installation.md)
- [Extension manager library for other browser extensions](pkg/extension-manager/README.md)
- [Composable browser adapter library](pkg/browser-adapter/README.md)
- [Use the `cmy` CLI](docs/cmy-cli.md)
- [Install extensions with the `cmy` CLI](docs/cmy-extension-cli.md)
- [Manage userscripts without an extension](docs/userscripts.md#extension-free-local-manager)
- [Target connection security](docs/target-connection-security.md)
- [Roadmap](docs/roadmap.md)
- [Jangolova render modules](docs/jangolova-render-modules.md)

## Development

```bash
go test ./...
npm run test:cymonkey
npm run test:browser-adapter
npm run test:extension-manager
npm run test:threejs-cymonkey
```
