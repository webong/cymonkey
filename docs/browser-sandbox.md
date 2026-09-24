# Browser sandbox integration

Some browser packages need an isolated document with a different content
security policy from an ordinary extension script. A consuming extension may
provide that sandbox and bundle reviewed package assets in its own build.

[`@cymonkey/browser-adapter`](../pkg/browser-adapter/README.md) provides
package validation, approvals, tab routing, and `createMediaBrokerController`
for tab-scoped camera sessions. `installEphemeralWebStorage` gives a sandboxed
document in-memory storage that disappears when the document closes.
`connectSandboxHost` and `startSandboxGuest` provide the nonce-bound
MessagePort handshake; the host creates the iframe and chooses its package.

The integrating extension supplies its own sandbox entrypoint, content
security policy, browser permissions, consent UI,
and offscreen document that actually obtains media. A sandbox must not receive
privileged extension APIs or a general remote-code loader. Camera Kit is an
example package that needs such a host; its runtime code is in
[`pkg/snapchat-camera-kit-cymonkey`](../pkg/snapchat-camera-kit-cymonkey/README.md).
