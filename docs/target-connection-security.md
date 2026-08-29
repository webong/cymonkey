# Target connection security

Jangolova resolves opaque target references immediately before an engine
connects. Resolved material exists only in the provider process and the
selected adapter or worker. It is not added to the target descriptor,
manifest, provider response, events, health output, logs, or process command
arguments.

This does not make Jangolova a secret store or network orchestrator. The
caller still owns secret storage, endpoint reachability, relays, tunnels,
placement, and target lifecycle.

## Resolution flow

1. The caller supplies `credentialRef` and/or `tlsRef` on an endpoint.
2. The provider selects an engine using protocols and capabilities.
3. A configured resolver obtains connection material for each reference.
4. Jangolova validates its shape and expiry, then gives an in-memory copy to
   the adapter.
5. Expiring credential and TLS leases are re-resolved before expiry.
6. The adapter acknowledges a generation only after it is active; Jangolova
   then releases the superseded resolver lease.
7. Disconnect releases remaining resolver leases and removes in-memory
   connection material.

Resolver failures contain only the reference name and a safe failure class.
Resolved header values are redacted from connection errors, calls, health
messages, lifecycle events, and provider shutdown errors.

## Material documents

Environment and directory resolvers consume strict documents matching the
[connection material schema](../protocol/target/v1/connection-material.schema.json).
A credential is a set of connection headers with a mandatory expiry:

```json
{
  "apiVersion": "interaction.connection/v1alpha1",
  "kind": "credential",
  "headers": {
    "Authorization": "Bearer short-lived-session-token"
  },
  "expiresAt": "2026-08-01T12:05:00Z"
}
```

Credentials already expired or expiring within five seconds are rejected.
Jangolova re-resolves expiring credential and TLS material before expiry and
accepts a replacement only when its expiry is later than the active
generation. Failed refreshes retry without replacing still-valid material.

Long-running HTTP adapters read the current generation before every request.
For a TLS change, WebDriver Classic and Safari MCP build an isolated candidate
transport and promote it only after a real request completes through the new
CA and client-certificate generation. The previous transport remains active
during validation and its idle connections are drained only after promotion.
A failed load or handshake leaves the old transport untouched and the new
generation unacknowledged.

CDP and BiDi adapters reconnect in place for header-only changes. A CA change
instead starts a new Node worker because Node reads `NODE_EXTRA_CA_CERTS` at
process startup. The adapter connects that candidate, atomically replaces the
worker behind the existing Jangolova interaction instance, and then stops the
old worker. The caller-owned browser is never replaced. Successful rotation
emits `interaction.connection.renewed`; a failed candidate emits
`interaction.connection.renewal_failed` and is retried. If no replacement
arrives before expiry, active health becomes unhealthy and HTTP requests are
rejected.

TLS material contains caller-managed absolute file paths:

```json
{
  "apiVersion": "interaction.connection/v1alpha1",
  "kind": "tls",
  "tls": {
    "caFile": "/run/secrets/browser-cluster-ca.pem",
    "clientCertificateFile": "/run/secrets/jangolova-client.pem",
    "clientKeyFile": "/run/secrets/jangolova-client-key.pem",
    "serverName": "browser.internal.example"
  },
  "expiresAt": "2026-08-02T00:00:00Z"
}
```

Client certificate and key paths must be supplied together. There is no
insecure-TLS option.

## Built-in resolvers

The environment resolver uses
`JANGOLOVA_<KIND>_<ENCODED_REFERENCE>`. ASCII letters and digits are uppercased;
other bytes are encoded as `_HH`, avoiding collisions. For example,
`browser-session` becomes:

```text
JANGOLOVA_CREDENTIAL_BROWSER_2DSESSION
```

Environment material is useful for development and tightly controlled process
supervisors. Production supervisors should normally mount files and set:

```text
JANGOLOVA_CONNECTION_MATERIAL_DIR=/run/jangolova-material
```

The directory layout is:

```text
/run/jangolova-material/
  credential/browser-session.json
  tls/browser-cluster-ca.json
```

The reference is validated as an identifier and never interpreted as a path.
Material files should be written to a temporary sibling and atomically renamed
over the active document so a resolver never observes a partial generation.
The environment resolver is attempted first, followed by the directory
resolver. Embedders inside the Jangolova distribution can inject a callback
resolver with `engineprovider.WithTargetResolver`, allowing a dedicated secret
manager without changing target or adapter contracts.

## Adapter support

| Adapter family | Header renewal | Live CA rotation | Live client-certificate rotation |
| --- | --- | --- | --- |
| Playwright CDP | In-process reconnect | Worker replacement | No |
| Puppeteer CDP/BiDi | In-process reconnect | Worker replacement | No |
| Web presentation CDP | In-process reconnect | Worker replacement | No |
| WebDriver Classic | Per request | Transport replacement | Transport replacement |
| Safari MCP HTTP | Per request | Transport replacement | Transport replacement |
| Cymonkey WebSocket | Transport replacement | Transport replacement | Transport replacement |

CDP workers authenticate both HTTP discovery and WebSocket attachment. Worker
processes receive headers over private standard input, never command-line
arguments. Node CDP workers reject mTLS explicitly because the supported
libraries do not expose a portable client-certificate hook; an authenticated
caller-owned relay remains the appropriate boundary for that case.

Node worker TLS material remains fixed for the lifetime of one worker process,
but the adapter replaces that process within the same interaction instance
when its CA generation changes. Cymonkey likewise creates a replacement
WebSocket transport, so both credential headers and TLS files are applied to
its candidate handshake before the previous transport is released.

The standalone command accepts matching endpoint reference flags:

```bash
jangolova connect-engine \
  --target-kind browser \
  --endpoint cdp=wss://browser.example/devtools/browser/42 \
  --credential-ref cdp=browser-session \
  --tls-ref cdp=browser-cluster-ca
```
