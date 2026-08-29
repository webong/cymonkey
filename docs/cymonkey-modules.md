# Cymonkey runtime and driver modules

Cymonkey is an open augmentation contract. The standalone core owns the public
module, policy, composite-routing, and conformance contracts. A host such as
Jangolova owns authentication, target lifecycle, credentials, and the
integration code that reaches a particular runtime or automation driver.

This is deliberately not a raw-plugin escape hatch. A module may expose only
typed Cymonkey capabilities through `hello`, `capabilities`, `describe`,
`act`, and `events`. It cannot gain permission to launch, stop, discover, or
arbitrarily inspect caller-owned targets.

## Vocabulary

| Term | Meaning | Examples |
| --- | --- | --- |
| `domain` | Where an operation takes effect. | `computer`, `render`, `player` |
| `runtime` | The concrete augmentable implementation. | `browser-dom`, `macos-app`, `godot` |
| `driver` | The implementation that translates Cymonkey operations for a target. | `playwright`, `puppeteer`, `macos-cooperative`, `godot-ws` |
| `transport` | The wire mechanism used by a driver. | `cdp`, `webdriver-bidi`, `cymonkey-ws`, Apple Events |
| `module` | A versioned Cymonkey contribution that provides a runtime or driver. | `browser.playwright`, `render.godot` |

Playwright and Puppeteer are drivers. CDP and WebDriver BiDi are transports
they use. A browser tab is the caller-owned target; `browser-dom` is the
runtime that a browser driver augments.

```text
caller-owned browser tab
└── computer / browser-dom
    ├── driver: playwright
    └── transport: cdp
```

## Module contract

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
| `computer.playwright` | driver | `computer/browser-dom` | `playwright` | `cdp` |
| `computer.puppeteer` | driver | `computer/browser-dom` | `puppeteer` | `cdp`, `webdriver-bidi` |
| `computer.cdp` | driver | `computer/browser-dom` | `cdp` | `cdp` |
| `computer.bidi` | driver | `computer/browser-dom` | `bidi` | `webdriver-bidi` |
| `computer.safari-mcp` | driver | `computer/browser-dom` | `safari-mcp` | `mcp-streamable-http` |
| `computer.macos` | runtime | `computer/macos-app` | `macos-cooperative` | authenticated local WebSocket to helper |
| `render.godot` | runtime | `render/godot` | `godot-ws` | `cymonkey-ws` |

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
      { "id": "desktop", "module": "computer.macos" },
      {
        "id": "scene",
        "module": "render.godot",
        "target": {
          "kind": "godot",
          "endpoints": [{ "protocol": "cymonkey-ws", "url": "ws://127.0.0.1:9321" }]
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
