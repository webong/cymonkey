---
name: blockade-provider-adapter
description: Create or update Blockade-owned custom vision/VLM provider adapters that map provider responses to Blockade observations. Use when adding a hosted VLM, cloud-vision endpoint, or provider-specific response mapping; do not use for Blockade local inference engines or Jangolova target interaction.
---

# Blockade Provider Adapter

Blockade owns provider-adapter discovery, lifecycle, credentials, routing,
and normalized observations alongside its local inference engines. Cymonkey
coordinates capture and transports pixels to Blockade; Jangolova captures or
presents pixels through its normal authorized interaction surface.

## Before implementation

1. Inspect repository instructions, the current diff, and the existing
   Cymonkey/Blockade extension points. Preserve unrelated work.
2. Read [the adapter contract](references/adapter-contract.md).
3. Obtain the provider's current official API documentation. Identify the
   image input format, structured-output support, authentication method,
   limits, timeout behavior, and error model before choosing a client or
   transport.
4. Read [the mapping rules](references/provider-mapping.md) before translating
   provider-native output.

## Choose the smallest correct integration

- For a mapping-only request, implement and test a provider response mapper;
  do not also add registry, deployment, or credential-management features.
- For an end-to-end provider integration, implement `blockade.ProviderAdapter`
  and register its factory by kind with `blockade.ProviderAdapterRegistry`.
  Do not add a provider-specific HTTP/auth branch to Blockade core.
- Keep provider packages under Blockade's adapter area. Provider SDKs, API
  keys, endpoints, model names, retry rules, and quotas must not enter
  Blockade's local inference core.
- Prefer an isolated subprocess or existing adapter service boundary when a
  provider SDK has a large dependency tree or conflicting runtime needs.

## Implementation requirements

- Preserve the Blockade request ID and original image bytes unless the
  provider requires a documented conversion.
- Propagate cancellation and apply a bounded timeout. Retry only operations
  known to be safe and only for transient failures; honor provider rate-limit
  guidance.
- Declare logical secret names under `providerAdapters[].secrets` using only
  `env` references, then resolve them through `ProviderAdapterRuntime.Secrets`.
  Never place tokens in settings, manifests, fixtures, logs, evidence, or
  returned errors.
- Request structured provider output when supported. Treat malformed or
  incomplete output as an adapter error rather than returning fabricated
  observations.
- Validate every mapped response with Blockade's current validation function
  before returning it.
- Keep provider errors distinguishable as authentication, rate limit,
  timeout/cancellation, unavailable, and invalid response where the surrounding
  error model permits. Do not turn failures into observations.

## Verification

Add redacted provider-response fixtures and focused tests for:

- request serialization and authentication without exposing the secret;
- successful coordinate, label, confidence, mask, and evidence mapping;
- malformed structured output and out-of-range values;
- cancellation, timeout, rate limiting, and non-success HTTP responses;
- final Blockade contract validation.

Use a local fake server or in-memory client in ordinary tests. Do not make a
paid or credentialed live provider call unless the user explicitly requests
and authorizes it. Update the provider example configuration and focused
documentation when the adapter is registered in a provider-enabled Blockade
build. Cymonkey must remain provider-neutral.

In the handoff, state which layer owns the adapter, which provider capabilities
are mapped or intentionally unsupported, what verification ran, and whether a
real-provider smoke test remains gated.
