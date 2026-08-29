# Jangolova Cymonkey for Unity

Add `CymonkeyBridge` and a transport-host component to a scene object, then assign
the host to the bridge's serialized `transportHost` field. Populate the
bridge's `registrations` list. Every entry needs a stable kind-prefixed ID, an
explicit Unity target, and an explicit action allowlist. Cymonkey never scans or
exports unregistered GameObjects.

The initial `CymonkeyWebSocketHost` binding uses `websocket`. The target owner
supplies `JANGOLOVA_CYMONKEY_PREFIX` (an `HttpListener` prefix,
for example `http://127.0.0.1:8090/cymonkey/`) and
`JANGOLOVA_CYMONKEY_TOKEN` through a secret injection mechanism. It then gives
Jangolova the corresponding `ws://...` `websocket` target endpoint and an
opaque credential reference that resolves to the Authorization header.

The package is a minimal MVP for Unity 2022.3 using the .NET 4.x API profile.
Projects targeting platforms without `HttpListener` WebSocket support can
provide another `ICymonkeyTransportHost` that preserves the same six protocol
methods. `CymonkeyBridge` contains no WebSocket startup or listener policy. The
semantic registry is transport-neutral. Closing Jangolova only closes its
transport; the Unity application continues running and rendering.
