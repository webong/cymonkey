# Cymonkey v1alpha2 Protocol Bindings

Generated Go bindings for the `cymonkey/v1alpha1` protocol.

## Regeneration

```bash
npm run generate:cymonkey-protocol   # regenerate from schema
npm run check:cymonkey-protocol      # verify generated files are up to date
```

Source schema: `src/protocol/cymonkey/v1alpha2/protocol.schema.json`

## Contents

- Protocol version constant: `ProtocolVersion`
- Typed enums: `DomainName`, `DriverName`, `SupportMode`, `Lifetime`, `Persistence`, `Effect`
- Message structs: `Hello`, `Capability`, `Surface`, `Augmentation`, `Description`, `Action`, `Event`, `EventBatch`
- `Client` / `Transport` interface for typed dispatch

These bindings are private to the source tree and are consumed by the Cymonkey
adapter and host implementation. External clients should use their own generated
wire types or the public Jangolova SDK rather than importing this `internal`
package.
