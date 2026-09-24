# Cymonkey runtime entry and Jangolova library modules

Cymonkey is the open runtime-entry and augmentation contract. Jangolova owns
the runtime-library and driver modules that execute semantic work after
Cymonkey has entered an approved target. Cymonkey supplies mounting and routing;
Jangolova supplies the integration code, authentication, target lifecycle, and
credentials for a particular runtime or automation driver.

This is deliberately not a raw-plugin escape hatch. A module may expose only
typed Cymonkey capabilities through `hello`, `capabilities`, `describe`,
`act`, and `events`. It cannot gain permission to launch, stop, discover, or
arbitrarily inspect caller-owned targets.

## Vocabulary

| Term | Meaning | Examples |
| --- | --- | --- |
| `domain` | Where an operation takes effect. | `viewer`, `render`, `player` |
| `runtime` | The concrete augmentable implementation. | `browser-dom`, `macos-app`, `godot` |
| `driver` | The implementation that translates Cymonkey operations for a target. | `playwright`, `puppeteer`, `macos-cooperative`, `websocket` |
| `transport` | The wire mechanism used by a driver. | `cdp`, `webdriver-bidi`, `websocket`, Apple Events |
| `module` | A versioned Jangolova contribution mounted or reached through Cymonkey. | `browser.playwright`, `render.godot` |

Playwright and Puppeteer are drivers. CDP and WebDriver BiDi are transports
they use. A browser tab is the caller-owned target; `browser-dom` is the
runtime that a browser driver augments.

```text
caller-owned browser tab
├── viewer / browser-dom (window and platform work)
└── render / browser-dom (document and augmentation work)
    ├── driver: playwright
    └── transport: cdp
```

## Module contract

In-repository Go modules import the private core at
`cymonkey/src/internal/core`; there is intentionally no public
re-export package. External runtime distributions integrate through the
Jangolova module/registry boundary instead of depending on the host's private
Go implementation.

A Go contributor implements and registers a module with a stable module ID,
descriptor, target compatibility check, and attachment function. The
descriptor declares its module kind, supported domain/runtime pairs, driver
IDs, transports, and capability contract version. Attachment receives an
already-resolved caller-owned target and returns a Cymonkey caller; it does
not provision a target.

```go
module := cymonkey.ModuleFunc{
    ModuleDescriptor: cymonkey.ModuleDescriptor{
        ID: "render.example-engine",
        Kind: cymonkey.RuntimeModule,
        Runtimes: []cymonkey.RuntimeBinding{{
            Domain: cymonkey.DomainRender,
            Runtime: "example-engine",
        }},
        Drivers: []cymonkey.DriverDescriptor{{
            ID: "example-engine-rpc",
            Transports: []string{"example-rpc"},
        }},
    },
    CompatibleTarget: func(target cymonkey.Target) bool {
        return target.Kind == "example-engine"
    },
    AttachTarget: attachExampleEngine,
}

registry, err := cymonkey.NewRegistry(module)
```

`NewRegistry` creates a host-controlled allowlist. A host decides which
compiled modules to register and how it will attach them to a caller-owned
target. There is no unreviewed download-and-execute plugin loader.

## Built-in modules

| Module | Kind | Runtime | Driver | Transport |
| --- | --- | --- | --- | --- |
| `browser.playwright` | driver | `viewer` + `render` / `browser-dom` | `playwright` | `cdp` |
| `browser.puppeteer` | driver | `viewer` + `render` / `browser-dom` | `puppeteer` | `cdp`, `webdriver-bidi` |
| `browser.cdp` | driver | `viewer` + `render` / `browser-dom` | `cdp` | `cdp` |
| `browser.bidi` | driver | `viewer` + `render` / `browser-dom` | `bidi` | `webdriver-bidi` |
| `browser.safari-mcp` | driver | `viewer` + `render` / `browser-dom` | `safari-mcp` | `mcp-streamable-http` |
| `viewer.macos` | runtime | `viewer/macos-app` | `macos-cooperative` | authenticated local WebSocket to helper |
| `render.godot` | runtime | `render/godot` | `websocket` | `websocket` |

The current macOS helper may map an action through Apple Events or
Accessibility after user consent. Those are helper implementation details;
the attachment driver remains `macos-cooperative`.

## Composite attachments

A composite is a list of module bindings. One binding uses the Adapter's root
target; every other binding includes its own caller-supplied target descriptor.
The coordinator attaches each module, merges their semantic contract, and
routes an action by its advertised `domain` and `runtime`.

```json
{
  "composite": {
    "bindings": [
      { "id": "desktop", "module": "viewer.macos" },
      {
        "id": "scene",
        "module": "render.godot",
        "target": {
          "kind": "godot",
          "endpoints": [{ "protocol": "websocket", "url": "ws://127.0.0.1:9321" }]
        }
      }
    ]
  }
}
```

Policies are applied after capability negotiation. A binding cannot advertise
or execute a filtered capability, even if its underlying runtime supports it.

## Conformance and distribution

Every module must pass the shared Cymonkey conformance suite. It validates the
five operations, descriptors, input schemas, policy filtering, selector-based
routing, event cursors, and caller-owned lifecycle. Module authors should ship
their package with a conformance fixture and an example target descriptor.

The Godot and macOS packages are the initial reference modules. Their
conformance fixtures are part of the Jangolova test suite and serve as the
templates for additional runtime and driver contributors.
