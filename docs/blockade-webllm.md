# Blockade WebLLM backend

The WebLLM backend adds browser-local, WebGPU-accelerated vision-language
inference to Blockade. It is an experimental backend behind the same public
`blockade.observation/v1alpha1` contract used by Ultralytics, ONNX Runtime, and
hosted-provider adapters.

## Boundary

```text
caller supplies PNG/JPEG bytes
  → Blockade webllm provider adapter
  → Blockade-owned loopback runtime bridge
  → Blockade-owned Chromium + dedicated Web Worker
  → WebLLM vision model on WebGPU
  → validated Blockade observations
```

The dedicated Chromium instance is not Jangolova and is not an interaction
browser. It uses an isolated Blockade profile, opens only Blockade's loopback
runtime page, and is never given target URLs, tab identifiers, credentials, or
action APIs. Cymonkey remains responsible only for coordinating capture,
routing pixels to Blockade, and preserving provenance.

## Configuration

The stock `blockade` executable registers adapter kind `webllm`:

```yaml
apiVersion: blockade.config/v1alpha1
providerAdapters:
  - id: browser-vlm
    kind: webllm
    timeout: 2m
    maxPayloadBytes: 8388608
    settings:
      model: Phi-3.5-vision-instruct-q4f16_1-MLC
      browserExecutable: /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
      cacheDirectory: /var/cache/blockade/webllm
      moduleURL: https://esm.run/@mlc-ai/web-llm@0.2.84
      maxTokens: "1024"
      contextWindowSize: "6144"
      headless: "true"
```

`model` is required. All other settings are optional:

- `browserExecutable` selects Google Chrome or Chromium. Blockade checks common
  executable names and standard macOS application paths when omitted.
- `cacheDirectory` is the dedicated browser profile holding WebLLM's model and
  WASM caches. It defaults to the operating system user-cache directory, split
  by adapter ID. Do not share one directory between concurrently running
  Blockade processes.
- `moduleURL` selects the WebLLM ES module and must be an absolute HTTPS URL.
  The default is pinned to version `0.2.84`; production deployments can use a
  trusted HTTPS mirror.
- `maxTokens` defaults to `1024` and is bounded to `4096`.
- `contextWindowSize` defaults to `6144` and is bounded to `32768`.
- `headless` defaults to `true`. Use `false` only when inspecting startup
  failures interactively.

WebLLM requires a WebGPU-capable Chromium installation. The first startup also
requires network access to the configured ES module and model artifacts unless
they are already cached.

## Operation

Build and start Blockade normally:

```sh
go build -o .cache/bin/blockade ./cmd/blockade
.cache/bin/blockade serve \
  --config infra/deploy/blockade/blockade.webllm.example.yaml \
  --inference browser-vlm \
  --bind 127.0.0.1:8091
```

Model initialization runs asynchronously in the inference browser. Poll the
normal Blockade health endpoint and only route traffic after it returns `200`:

```sh
curl --fail http://127.0.0.1:8091/healthz
```

Then use the ordinary `/v1/observe` API or `blockade observe`. Every request is
serialized through the loaded WebLLM engine, receives a fixed structured-JSON
schema, preserves its Blockade request ID, and records deterministic evidence
as `webllm:<model>`.

The loopback runtime is protected by a random per-process token. Blockade owns
the HTTP listener, browser process, worker, cache profile, request queue, and
shutdown lifecycle. Browser-side failures are mapped to Blockade's typed
provider errors; generated responses still pass the provider-adapter wrapper's
API-version, request-ID, confidence, region, and mask validation.

## Current limitations

- Vision support and model coverage are still evolving upstream, so this
  backend is not the default production detector.
- The maintained default is the official Phi 3.5 vision example model. Other
  configured model IDs must support image messages in WebLLM.
- A cold cache can take substantially longer than an ordinary observation
  timeout. Start the service early and gate traffic on health instead of using
  one-shot observation for initial model download.
- VLM regions are model-generated and generally less geometrically reliable
  than YOLO/ONNX detections. Use ONNX or Ultralytics where precise boxes or
  masks are required.
- PNG and JPEG inputs are supported. WebLLM does not provide Blockade masks in
  this adapter.

The implementation follows WebLLM's official
[vision-model example](https://github.com/mlc-ai/web-llm/tree/main/examples/vision-model),
[worker API](https://webllm.mlc.ai/docs/user/advanced_usage.html), and
[structured-generation interface](https://github.com/mlc-ai/web-llm).

An opt-in end-to-end smoke test exercises a real browser, WebGPU, model load,
multimodal generation, and response validation:

```sh
BLOCKADE_WEBLLM_SMOKE=1 \
  go test ./internal/blockadewebllm -run TestWebLLMFixtureSmoke -v
```

Set `BLOCKADE_WEBLLM_BROWSER`, `BLOCKADE_WEBLLM_CACHE`,
`BLOCKADE_WEBLLM_MODEL`, or `BLOCKADE_WEBLLM_MODULE_URL` to override the smoke
test defaults. Ordinary test runs skip this multi-gigabyte fixture.

A smaller boot probe validates Chromium launch, the loopback bridge, and the
pinned WebLLM module without downloading a model:

```sh
BLOCKADE_WEBLLM_BOOT_PROBE=1 \
  go test ./internal/blockadewebllm -run TestWebLLMRuntimeBootProbe -v
```
