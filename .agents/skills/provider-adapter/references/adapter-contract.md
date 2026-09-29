# Adapter contract and ownership

Read the current repository sources before implementing because the checked-in
schema is authoritative:

- `lib/blockade/protocol/v1alpha1/observation.schema.json`
- `lib/blockade/protocol/v1alpha1/provider-adapter.schema.json`
- `lib/blockade/protocol.go`
- `lib/blockade/provider_adapter.go`
- `lib/blockade/validate.go`
- `docs/blockade.md`
- `docs/subsystem-boundaries.md`

## Ownership

```text
agent or application
  -> Cymonkey coordinates capture when pixels come from an attached target
  -> Jangolova returns policy-authorized pixels when capture is needed
  -> Blockade selects a configured provider adapter and calls the custom VLM
  -> Blockade maps the result to blockade.observation/v1alpha1
  -> caller receives observations and decides on any later action
```

- **Cymonkey:** capture coordination, provenance, and transport of observation
  requests and results.
- **Blockade:** provider adapter registration/discovery, lifecycle, endpoint
  selection, provider configuration, secret injection, timeouts, normalized
  request/response types, validation, and local Ultralytics/ONNX engines.
- **Jangolova:** interaction and presentation against caller-owned targets. It
  neither configures nor calls custom VLM providers.

Blockade is the provider-adapter boundary. Provider clients and credentials
belong there, isolated from its local inference core.

## Request behavior

The current request carries an API version, request ID, encoded image bytes,
and an optional prompt. An adapter must:

- preserve a supplied request ID through the response;
- send the image only to the selected provider endpoint;
- preserve the prompt's meaning while adding only the structured-output
  instructions needed by the mapping;
- avoid recompression or resizing unless required by a documented provider
  constraint or explicitly configured by the operator.

## Response behavior

Return `blockade.observation/v1alpha1` and validate it using
`blockade.ValidateObserveResponse` (or the current equivalent). Each
observation currently requires:

- `kind`
- `label`
- `confidence` in `[0,1]`
- a pixel-space `region` with non-negative width and height

An optional mask is a base64-encoded PNG. Evidence is concise provenance, not
a raw provider payload or chain-of-thought.

If the provider cannot produce enough information for a contract-valid
observation, return a typed adapter error. Do not invent missing geometry or
confidence silently.

## Provider-neutral extension point

Implement `blockade.ProviderAdapter` and register a factory by kind with
`blockade.ProviderAdapterRegistry`. The adapter must provide:

- a versioned request/response envelope based on the Blockade contract;
- capability metadata such as detection, grounding, OCR, description, and
  segmentation;
- bounded request timeout and payload limits;
- non-secret provider/model settings owned by Blockade;
- environment-only secret references resolved through the runtime secret
  source;
- health/readiness that does not expose credentials;
- deterministic shutdown under Blockade supervision.

The Blockade wrapper enforces the configured request timeout and payload limit,
preserves request IDs, validates every response, and closes the adapter after
active calls finish. Adapter methods must be safe for concurrent use until
close begins. Do not design a provider catalog, marketplace, dynamic code
download, or new agent framework merely to add one adapter.
