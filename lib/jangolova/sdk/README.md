# Jangolova public SDK

Runtime modules import `jangolova/sdk` and
`jangolova/contract`. The integration entry point is
`jangolova` (package `jangolova`). These public packages have no
dependency on the operator's private packages.

## Runtime registration

```go
type Backend interface {
    Name() jangolova.BackendName
    Domains() []contract.Domain
    Compatible(sdk.EngineTarget) bool
    Connect(context.Context, sdk.EngineSpec, sdk.EngineTarget,
        jangolova.Options) (sdk.EngineInstance, error)
}

adapter := jangolova.Adapter{
    Host: hostServices,
    Backends: []jangolova.Backend{reviewedBackend},
}
```

The host supplies the target; registering a backend does not launch it. Modules
may implement `sdk.Caller`, `EngineCapabilityProvider`, `EngineHealthProvider`,
`EngineEventSource`, and `EngineCallerLaunchProvider` on the returned instance.
`Disconnect` releases the integration's resources and leaves the caller-owned
application running. Connection lifecycle events are distinct from the
runtime's cursor-based `events` response.

`Options` carries host-supplied limits in `PolicyLimits`. A module may narrow
these limits but must not reinterpret them as approval to exceed operator
policy. `Authorize` reports module support/limits; the calling host remains
responsible for approval before a semantic call.

## Host injection

`sdk.Host` supplies only the services the chosen backend needs:

- `ValidateEndpoint`: enforce host-approved endpoint/material scope and expiry.
- `WorkerEnvironment`, `StartWorker`: provide a bounded worker process.
- `DialWebSocket`: establish a credential-aware runtime connection.
- `ListenWebSocket`: accept a cooperative helper connection.
- `ConnectSafari`: supply an existing Safari MCP attachment.
- `Redact`: scrub private material from host-facing failures.

Omitted services fail with an explicit error. The library does not discover an
implicit privileged host. The material interface exposes a snapshot and revision
notifications; it does not expose the private credential store or rotation API.

Cymonkey's binding is implemented by `lib/jangolova/host`. The operator injects
its private services through `../../src/internal/hostbinding`; third-party
modules supply their own public backend and never import that private binding.

## Discovery and activation

Use `registry.Discover`, `Registry.Select`, and `registry.Pull` to retrieve one
reviewed artifact. A digest match establishes correspondence to the selected
metadata, not independent publisher authenticity. The host must trust/approve
the registry and artifact identity.

```go
session, err := registry.Activate(ctx, module, platform, cachedPath,
    registry.Activation{
        Approve: approveModuleAndDigest,
        Mount: mountVerifiedBytesIntoOwnedRuntime,
    })
```

`Approve(context.Context, registry.Module, registry.Artifact) error` is required.
`Mount(context.Context, registry.Module, io.Reader) (sdk.EngineInstance, error)`
receives the bytes whose digest was checked, rather than reopening a mutable
cache path. `MaxBytes` defaults to 64 MiB for activation; set an explicit bound
for a larger reviewed artifact. Metadata is copied separately for each callback.

Activation verifies protocol/runtime identity and declared actions through
`hello` and `capabilities`, then checks `describe` and `health`. A failed
negotiation disconnects the returned module. After activation the host may call
advertised actions and consume cursor-based events. Runtime resource allowlists
remain authoritative.

## Compatibility

The wire version remains `cymonkey/v1alpha1`. Native WebSocket integration
supports the Blender authentication notice before an RPC reply. The host must
supply authentication during the upgrade; an authorization-required notice
fails the connection rather than granting access.

The SDK has not been released as a separate Go module. See the
[validation record](../../../docs/jangolova-runtime-validation.md) for actual
engine runs, contract-only checks, and remaining platform limitations.
