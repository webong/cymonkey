# Cymonkey extraction boundary

This repository contains two products with a deliberate dependency direction:

```text
Cymonkey core  ─────────────► no Jangolova dependency
       ▲
       │ protocol, modules, composition, conformance
       │
Jangolova host ─────────────► depends on Cymonkey core
       │
       ├── target lifecycle and credentials
       ├── browser drivers: CDP, BiDi, Playwright, Puppeteer, Safari MCP
       ├── Jangolova Browser Extension
       └── macOS app, cooperative helper, Safari and userscript systems
```

## Cymonkey core

The `src/cymonkey/` directory is the extraction-ready core. It uses only the
standard library and contains:

- the versioned semantic protocol and validation;
- public target, endpoint, policy, caller, attachment, module, and registry
  contracts;
- composite capability routing and event cursor merging;
- module conformance validation and fixtures.

Its local [README](../src/cymonkey/README.md) is the contributor entry point.

It must not contain a browser binary, extension, Node worker, Apple Event,
Accessibility request, Xallet reference, Jangolova internal import, target
launcher, credential resolver, or product-specific runtime module.

The existing wire identifier, `jangolova.cymonkey/v1alpha2`, is a protocol
identity rather than a Go-package dependency. A future independent Cymonkey
release may deliberately version that identifier; doing so is a protocol
change, not a prerequisite for extracting the core.

Once its API stabilizes, this directory can move unchanged into the dedicated
`cymonkey` repository and receive its own Go module path.

## Jangolova integration layer

Jangolova's integration layer, in `integrations/jangolova/cymonkey/`, adapts
caller-owned target and lifecycle contracts to the Cymonkey core and hosts the
built-in integration modules.
Those modules are products of Jangolova, not requirements of Cymonkey:

| Integration | Jangolova responsibility |
| --- | --- |
| Playwright / Puppeteer | Attach to caller-provided CDP or BiDi browser endpoints. |
| CDP / BiDi / Safari MCP | Map browser protocols to semantic Cymonkey operations. |
| Browser Extension | Provide privileged persistence and browser-native facilities. |
| macOS helper | Obtain user-consented Apple Events / Accessibility capability. |
| Godot | Provide an optional reference runtime module over `cymonkey-ws`. |

The integration layer may import Jangolova internals. The core may not.

## Migration sequence

1. Establish and test the dependency-free core contracts in `src/cymonkey/`.
2. Point Jangolova integrations at those contracts through a small host bridge.
3. Move built-in browser, macOS, and Godot modules out of the core path.
4. Publish or split the `src/cymonkey/` directory as its own repository only after
   the Jangolova bridge is the sole remaining dependency edge.

There is no requirement for a dynamic binary plugin loader. Hosts explicitly
register reviewed, compiled modules; third parties can distribute their own
modules against the public Cymonkey SDK.
