# Cymonkey–Jangolova integration boundary

Cymonkey owns operator policy, approval, credential resolution, coordination,
and host resource management. Jangolova owns target/runtime modules, protocol
negotiation, and semantic actions. Blockade owns visual inference and cloud/VLM
adapters. Jangolova does not configure or call Blockade.

## Public module interface

| Path | Responsibility |
| --- | --- |
| `src/jangolova/contract` | Public runtime wire data and validation. |
| `src/jangolova/sdk` | Target/spec/session types and injected host service interfaces. |
| `lib/jangolova` | Runtime selection and browser/native semantic adapters. |
| `src/jangolova/registry` | Metadata discovery, verified retrieval, and explicit activation. |
| `internal/jangolovahost` | Cymonkey-owned implementation of SDK host services and private/public data conversion. |
| `internal/cymonkeycore` | Cymonkey's private composition and coordination implementation. |
| `pkg/` | Distributable runtime packages. |

`lib/jangolova` imports public Jangolova packages and has no transitive
dependency on `cymonkey/internal`, the private adapter implementations, or
`targetconn`. `TestPublicIntegrationHasNoPrivateDependencies` checks this graph.

Public signatures define their own data and interfaces. They are not aliases
to private orchestrator, manifest, bridge, or worker implementations. The
credential view permits reading a snapshot, observing revisions, and
acknowledging a revision; rotation, persistence, expiry enforcement, and secret
cleanup remain host responsibilities.

The host supplies worker startup, credential-aware connection setup, cooperative
listeners, and the legacy Safari connection. Missing services fail explicitly.
`internal/jangolovahost.Services` binds those operations to Cymonkey internals;
`Wrap` converts the public adapter/session into the private provider contract.
The standalone `jangolova` executable receives this binding through its builtin
registry, preserving the same browser/helper behavior.

## Adding a runtime

Implement `jangolova.Backend` using `sdk.EngineSpec`, `sdk.EngineTarget`, and
`jangolova.Options`. Register it explicitly through `Adapter.Backends` after the
host approves the module. A backend identifies its domains, target kinds, and
endpoint protocol. This supports a contributor's target such as Lens Studio
without adding an engine-specific case to the operator.

The host owns approval. A module's `Authorize` method checks support and
host-supplied limits; a positive result is not an operator approval. Runtime
packages still enforce explicit resource and per-resource action registration.
Discovery never scans an application's objects or executes discovered code.

The existing wire identifier `cymonkey/v1alpha1` is retained for deployed engine
compatibility. It is not a dependency on Cymonkey's private Go implementation.
Changing the wire name requires a separately versioned protocol migration.

## Distribution and remaining work

The public SDK is currently in this repository's Go module; it has not yet been
published as an independently versioned SDK module. A host loads reviewed
artifacts through `registry.Activate`, supplying an approval callback and a
runtime-specific mount callback. The SDK does not implement a universal binary
plugin loader, install editor licenses, or own target process lifecycle.

See [the public SDK guide](../src/jangolova/sdk/README.md),
[module discovery and activation](jangolova-module-registry.md), and
[runtime validation record](jangolova-runtime-validation.md).
