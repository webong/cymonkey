# Content kit

The content kit is the host role for the `content.provider` contract. A process
that wants to run content providers embeds it: the host (`src`) can, and so can
a guest that hosts other plugins, such as a content service guest.

It holds:

- the request, response, model and runtime documents (`content.observation/v1`,
  `content.item/v1`, `content.model/v1`, `content.adapter/v1`,
  `content.runtime/v1`, `content.source/v1`) and their JSON schemas;
- request and response validation;
- the provider adapter registry, configured engines, limits and error mapping;
- model and runtime manifests, digest verification and managed runtimes;
- `plugin/`, which opens an installed ext package that serves
  `content.provider` and enforces the admission policy for each worker.

It contains no inference engine, HTTP service, or product behaviour. Providers
(`pkg/content-onnx`, `pkg/content-webllm`, `pkg/content-endpoints`,
`pkg/content-ultralytics`) build against it as guests; the HTTP service, client
and the generation contracts live in `lib/jangolova/generationkit`.
