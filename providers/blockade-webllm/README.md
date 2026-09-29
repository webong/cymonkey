# Blockade WebLLM provider

This executable implements `blockade.provider@v1alpha1` with WebLLM in a
provider owned Chromium process and dedicated Web Worker.

```sh
go build -o /path/to/package/provider ./providers/blockade-webllm
```

Create `plugin.json` beside the binary with package name `webllm`, the
`blockade.provider@v1alpha1` contract, the host platform, and the executable's
SHA-256. Install it with `cymonkey plugins install`.

WebLLM uses a remote model ID in provider settings and its own browser cache.
It does not use Blockade's local model registry:

```yaml
apiVersion: blockade.config/v1alpha1
providers:
  - id: browser-vlm
    provider: webllm
    timeout: 2m
    settings:
      model: Phi-3.5-vision-instruct-q4f16_1-MLC
      maxTokens: "1024"
      contextWindowSize: "6144"
      headless: "true"
```

See [`docs/blockade-webllm.md`](../../docs/blockade-webllm.md) for all settings,
operation, cache behavior, tests, and limitations.
