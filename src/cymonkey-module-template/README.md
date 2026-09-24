# Cymonkey module template

Use this template as the starting point for a caller-approved Cymonkey runtime
or driver contribution. A module is compiled into the Cymonkey host and added
to an explicit `Registry`; it is not a downloaded or automatically executed
plugin.

This is an in-repository Go template. The core is private, so modules compiled
here import `cymonkey/src/internal/core` directly. There is intentionally
no public re-export façade.

```go
package example

import (
    "context"

    cymonkey "cymonkey/src/internal/core"
)

func Module() cymonkey.Module {
    return cymonkey.ModuleFunc{
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
            ProtocolVersion: cymonkey.ProtocolVersion,
        },
        CompatibleTarget: func(target cymonkey.Target) bool {
            return target.Kind == "example-engine"
        },
        AttachTarget: func(ctx context.Context, options cymonkey.AttachOptions) (cymonkey.Attachment, error) {
            // Connect only to target endpoints the caller already supplied.
            // Return an Attachment that also implements cymonkey.Caller.
            panic("implement attach")
        },
    }
}
```

`AttachOptions` contains the caller-owned target and policy—not worker paths,
extension identities, tokens, or raw configuration. Start from the Go contract
test in `src/internal/core/core_test.go`, then add a fixture that passes
`ValidateModuleConformance` for all five Cymonkey operations.

External Go modules cannot import this private core package. They should
integrate through the supported Jangolova module/registry boundary instead of
depending on Cymonkey's internal implementation.

See [the module contract](../../docs/cymonkey-modules.md) for the full policy,
composition, lifecycle, and conformance requirements.
