# Cymonkey v1alpha2 Protocol Bindings

Generated Go bindings for the `cymonkey/v1alpha1` protocol.

## Regeneration

```bash
npm run generate:cymonkey-protocol   # regenerate from schema
npm run check:cymonkey-protocol      # verify generated files are up to date
```

Source schema: `protocol/cymonkey/v1alpha2/protocol.schema.json`

## Contents

- Protocol version constant: `ProtocolVersion`
- Typed enums: `DomainName`, `DriverName`, `SupportMode`, `Lifetime`, `Persistence`, `Effect`
- Message structs: `Hello`, `Capability`, `Surface`, `Augmentation`, `Description`, `Action`, `Event`, `EventBatch`
- `Client` / `Transport` interface for typed dispatch

These bindings are consumed by the Cymonkey adapter and any external client
that needs version-pinned, schema-derived types without importing the full
adapter package.
