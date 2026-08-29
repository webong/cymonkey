# Cymonkey module template

Use this template as the starting point for a caller-approved Cymonkey runtime
or driver contribution. A module is compiled into the Jangolova host and added
to an explicit `ModuleRegistry`; it is not a downloaded or automatically
executed plugin.

```go
package example

import (
    "context"

    "jangolova/cymonkey"
)

func Module() cymonkey.Module {
    return cymonkey.ModuleFunc{
        Definition: cymonkey.ModuleDescriptor{
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
            ProtocolVersion: cymonkey.ProtocolVersion,
        },
        CompatibleTarget: func(target cymonkey.EngineTarget) bool {
            return target.Kind == "example-engine"
        },
        AttachTarget: func(ctx context.Context, spec cymonkey.EngineSpec, target cymonkey.EngineTarget, options cymonkey.AttachOptions) (cymonkey.EngineInstance, error) {
            // Connect only to target endpoints the caller already supplied.
            // Return an EngineInstance that also implements cymonkey.Caller.
            panic("implement attach")
        },
    }
}
```

`AttachOptions` is deliberately public but contains only the selected semantic
binding and policy—not worker paths, extension identities, tokens, or raw
configuration. Start from the Go contract test in
`src/cymonkey/modules_test.go`, then add a fixture that passes
`ValidateModuleConformance` for all five Cymonkey operations.

See [the module contract](../../docs/cymonkey-modules.md) for the full policy,
composition, lifecycle, and conformance requirements.
