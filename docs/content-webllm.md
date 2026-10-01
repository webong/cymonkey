# Cymonkey WebLLM content plugin

`providers/content-webllm` builds a Cymonkey content plugin for browser
local, WebGPU accelerated vision language inference. It speaks
`cymonkey.content@v1alpha1` and returns the same normalized image observation
contract as other content plugins.

## Boundary

```text
caller supplies PNG or JPEG bytes
  -> Cymonkey content service
  -> WebLLM plugin executable
  -> plugin owned loopback bridge
  -> plugin owned Chromium + dedicated Web Worker
  -> WebLLM vision model on WebGPU
  -> validated Cymonkey observations
```

The dedicated Chromium process is an inference runtime. It is not a display
target browser and receives no target URL, tab ID, user credential, or action
API.

## Install

Build the plugin and create `plugin.json` beside it using the executable's
real SHA-256:

```sh
go build -o /tmp/webllm-provider ./providers/content-webllm
cymy plugins install --manifest /path/to/webllm/plugin.json
```

The manifest name should be `webllm` and its contract should be
`cymonkey.content@v1alpha1`. See the
[plugin packaging guide](cymonkey-plugins.md) for the complete manifest.

## Configure

```yaml
apiVersion: cymonkey.content-config/v1alpha1
adapters:
  - id: browser-vlm
    plugin: webllm
    timeout: 2m
    maxPayloadBytes: 8388608
    settings:
      model: Phi-3.5-vision-instruct-q4f16_1-MLC
      browserExecutable: /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
      cacheDirectory: /var/cache/cymonkey/content/webllm
      moduleURL: https://esm.run/@mlc-ai/web-llm@0.2.84
      maxTokens: "1024"
      contextWindowSize: "6144"
      headless: "true"
```

`model` is required. It is WebLLM's remote model ID and does not use the local
Cymonkey local model store. Other settings are optional:

- `browserExecutable` selects Chrome or Chromium. Common executable names and
  macOS application paths are checked when omitted.
- `cacheDirectory` stores the dedicated browser profile and model caches. It
  defaults to the operating system cache directory, separated by instance ID.
- `moduleURL` must be an absolute HTTPS URL and defaults to the pinned WebLLM
  module shown above.
- `maxTokens` defaults to `1024` and is limited to `4096`.
- `contextWindowSize` defaults to `6144` and is limited to `32768`.
- `headless` defaults to `true`.

WebLLM needs a WebGPU capable Chromium installation. Its first start also needs
access to the configured module and model artifacts unless both are cached.

## Operate

```sh
cymy content serve \
  --config infra/deploy/content/content.webllm.example.yaml \
  --inference browser-vlm \
  --bind 127.0.0.1:8091
```

Model loading is asynchronous. Wait for `GET /healthz` to return HTTP 200
before sending traffic. Each request is serialized through the WebLLM engine,
uses a bounded structured JSON response, preserves the Cymonkey request ID,
and records evidence as `webllm:<model>`.

The loopback bridge uses a random token. The plugin owns the listener,
browser process, worker, cache profile, request queue, and shutdown lifecycle.
Cymonkey maps plugin failures to stable errors and validates the returned
API version, request ID, confidence values, regions, and masks.

## Limitations and tests

- The configured model must support image messages in WebLLM.
- Cold model downloads may exceed an ordinary observation timeout.
- VLM generated regions are less geometrically reliable than detector output.
- PNG and JPEG inputs are supported. This provider does not return masks.

Run the opt in browser tests from the provider module:

```sh
cd providers/content-webllm
CYMONKEY_CONTENT_WEBLLM_BOOT_PROBE=1 go test ./runtime -run TestWebLLMRuntimeBootProbe -v
CYMONKEY_CONTENT_WEBLLM_SMOKE=1 go test ./runtime -run TestWebLLMFixtureSmoke -v
```

The full smoke test downloads and runs a model. Ordinary test runs skip both
environment gated probes.
