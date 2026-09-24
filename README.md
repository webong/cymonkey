# Cymonkey

Cymonkey is the operating layer for AI systems that need to work with real
interfaces. It connects agents and applications to approved browsers, desktop
surfaces, and interactive runtimes; gives them structured capabilities instead
of unrestricted device access; and keeps every target under its owner's
control.

It supports both sides of interface work: operating an existing experience and
presenting a new dynamic one.

## Capabilities

- Connect to browsers through CDP, WebDriver BiDi, WebDriver Classic, Safari
  MCP, WebExtension, and native integrations.
- Mount reviewed browser packages such as **Three.js Scene** into an approved
  tab.
- Expose typed, policy-governed actions for navigation, interaction, display,
  presentation, and runtime-specific scene controls.
- Operate explicit Three.js, Godot, Unity, Unreal, and Blender resources through their
  runtime libraries.
- Request visual evidence through Blockade, including local YOLO, SAM, and ONNX
  inference.
- Preserve target ownership: a connection may detach, but it never quits a
  browser, terminates an application, or destroys a scene.

## Architecture

```text
agent or application
  → Cymonkey
    → Jangolova interaction and presentation runtime
      → browser, desktop surface, Three.js, Godot, Unity, Unreal, or Blender
    → Blockade visual inference
```

Jangolova provides the runtime-facing interaction and presentation tools.
Blockade provides visual inference. Cymonkey hosts and coordinates those
capabilities into one controlled operating surface.

## Quick start

Build the executables and start Cymonkey:

```bash
mkdir -p .cache/bin
go build -o .cache/bin/cymonkey ./src

export CYMONKEY_JANGOLOVA_TOKEN="replace-with-a-random-secret"
PATH="$PWD/.cache/bin:$PATH" cymonkey run \
  --config infra/deploy/cymonkey/cymonkey.example.yaml
```

The host manifest starts the interaction provider and visual inference service.
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
- [Architecture](docs/architecture.md)
- [Browser packages](docs/browser-augmentation-packages.md)
- [Browser extension manager](docs/browser-extension-installation.md)
- [Target connection security](docs/target-connection-security.md)
- [Roadmap](docs/roadmap.md)
- [Jangolova render modules](docs/jangolova-render-modules.md)

## Development

```bash
go test ./...
npm run test:cymonkey
npm run test:browser-extension
npm run test:threejs-cymonkey
```
