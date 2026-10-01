# Cymonkey WebLLM content plugin

This executable implements `cymonkey.content@v1alpha1` with WebLLM in a
plugin owned Chromium process and dedicated Web Worker.

```sh
go build -o /path/to/package/provider ./providers/content-webllm
```

Create `plugin.json` beside the binary with package name `webllm`, the
`cymonkey.content@v1alpha1` contract, the host platform, and the executable's
SHA-256. Install it with `cymy plugins install`.

WebLLM uses a remote model ID in plugin settings and its own browser cache. It
does not use Cymonkey's local model store:

```yaml
apiVersion: cymonkey.content-config/v1alpha1
adapters:
  - id: browser-vlm
    plugin: webllm
    timeout: 2m
    settings:
      model: Phi-3.5-vision-instruct-q4f16_1-MLC
      maxTokens: "1024"
      contextWindowSize: "6144"
      headless: "true"
```

See [`docs/content-webllm.md`](../../docs/content-webllm.md) for all settings,
operation, cache behavior, tests, and limitations.
