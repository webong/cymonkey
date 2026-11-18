# Content kit

The content kit is the shared content contract: what the host, the content
engine and every content provider must agree on.

- the content documents and their JSON schemas (`content.observation/v1`,
  `content.item/v1`, `content.model/v1`, `content.adapter/v1`);
- validation of requests, observations and responses;
- the provider adapter types (`ProviderAdapter`, requests, responses,
  capabilities, health, runtime) and typed errors;
- the model manifest types and name, version and digest rules.

It holds no behaviour that launches or supervises a provider. The content
engine, which does (adapter registry, configured engines, model and runtime
stores, the `content.provider` plugin adapter and admission policy, the HTTP
service), is `pkg/console/contentengine`.
