---
name: jangolova-blockade-cloud-provider
description: Implement a community cloud vision provider for Jangolova Blockade observations through the Grimlock VisionProvider boundary (fal.ai, Replicate, OpenAI-compatible VLMs, or any hosted detection or segmentation API). Use when adding a cloud adapter, mapping provider responses to blockade.observation/v1alpha1, or registering a custom vision provider.
---

# Jangolova Blockade Cloud Provider

Use this skill when an agent must add a cloud vision provider so Grimlock can
observe pixels through a hosted model. Cloud adapters are community-owned:
first-party Jangolova ships only the interface, the contract, and this guide.

## Ownership rules

- **Grimlock owns** provider registration, credentials, provider-specific
  request mapping, timeouts, retries, and provider switching.
- **Blockade owns** normalized observation types
  (`blockade.observation/v1alpha1`) and local inference engines.
- Never put credentials, endpoints, or account identifiers into Blockade YAML,
  model paths, committed files, logs, or chat output. Resolve secrets from the
  host environment at runtime.

## Interface

Implement `grimlock.VisionProvider`
([internal/grimlock/vision.go](../../internal/grimlock/vision.go)):

```go
type VisionProvider interface {
    Protocol() string
    Observe(context.Context, blockade.ObserveRequest) (blockade.ObserveResponse, error)
}
```

Register it when constructing the service:

```go
registry, _ := grimlock.NewVisionProviderRegistry(myProvider)
grimlock.WithVisionProviderRegistry(registry)
```

## Standard implementation sequence

1. Read the contract: [protocol/blockade/v1alpha1/observation.schema.json](../../protocol/blockade/v1alpha1/observation.schema.json).
2. Follow [references/provider-playbook.md](references/provider-playbook.md)
   for a working HTTP adapter skeleton, response-mapping table, and test
   template.
3. Map every provider result to `Observation`, `Region`, and optional base64
   PNG `mask`. Raw provider payloads are evidence at most; they are never
   returned as-is.
4. Validate before returning: `blockade.ValidateObserveResponse(response)`.
5. Add tests: unit-test the mapper with recorded fixtures; gate any live-call
   test behind an environment variable exactly like
   `TestOnnxEngineFixtureSmoke`.

## Verification checklist

- `go test ./internal/grimlock ./internal/blockade` passes.
- Mapper handles empty results, undecodable images, and provider errors
  without leaking request IDs, URLs, or credentials into errors.
- Confidence values are clamped to `[0,1]`; regions are non-negative and in
  original pixel coordinates.
- No secret ever reaches disk, logs, or Git.
- The repository boundary test still passes: never create directories named
  `connector`, `session`, `surface`, `vnc`, or similar under product roots.

Never commit provider API keys. Never log full provider responses containing
account metadata.
