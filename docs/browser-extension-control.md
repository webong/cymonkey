# Browser extension control protocol

`cymonkey.browser-extension/v1alpha1` is an optional protocol for an extension
that integrates Cymonkey. Cymonkey provides its schema, generated bindings,
policy functions, and an outbound control client as libraries.

An integrating extension authenticates callers and resolves target context
from trusted browser APIs. `createControlGate` then authorizes calls by caller,
effect, capability, target origin/tab, and augmentation ID. It defaults to
deny and emits audit phases with metadata only. The extension may configure
`OutboundControlClient` with its own socket and storage dependencies; its
status description omits the token.

The canonical schema and recorded exchanges live in
`src/protocol/browser-extension/v1alpha1/`. Regenerate the TypeScript and Go
bindings with:

```sh
npm run generate:browser-extension-protocol
npm run check:browser-extension-protocol
```

The TypeScript binding is generated into `pkg/browser-adapter/src/generated/`.
No website should receive this private protocol or its credentials.
