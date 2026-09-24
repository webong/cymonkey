# Blockade

Blockade is a standalone read-only visual-inference service. It runs local
pixel-oriented models and returns normalized detections, regions, masks, and
confidence. It does not plan, interact with targets, host an agent, or depend
on Cymonkey or Jangolova.

## Ownership boundary

```text
Cymonkey or standalone caller
  ├─ supplies pixels
  └─ decides what to do with observations
          ↓ blockade.observation/v1alpha1
Blockade
  ├─ selects and runs configured local engines or hosted-provider adapters
  ├─ owns adapter configuration, credentials, and provider-native mapping
  └─ returns normalized observations
```

Blockade owns cloud-provider and VLM integration through pluggable adapters,
not hardcoded provider branches. It owns adapter configuration, credentials,
and provider-native request/response mapping. Cymonkey exposes its coordinated
observation capability directly and delegates only the image inference request
to Blockade.

```text
pixels → configured Blockade inference backend → observations → caller decision
```

Blockade has no Cymonkey dependency. It does not know whether a supplied image
came from a browser, desktop application, renderer, file, or camera, and it
never initiates an action from an observation.

## Cymonkey/Jangolova composition

Cymonkey may supervise the standalone `blockade` executable alongside the
standalone `jangolova` tool server. Blockade remains usable without Cymonkey:

```sh
cymonkey blockade serve --config infra/deploy/blockade/blockade.example.yaml
```

The host only manages process lifecycle and composition. It does not absorb
Blockade's model configuration or provider-specific inference behavior.

When an external agent requests a coordinated observation, Cymonkey is the
context switcher:

```text
external agent
  → Cymonkey observation coordinator
  → Jangolova policy-authorized window.screenshot
  → pixels only
  → Blockade ObserveRequest
  → normalized Blockade ObserveResponse
  → Cymonkey provenance envelope
  → external agent decides whether to request a later Cymonkey action
```

`cymonkey observe` calls Jangolova's standard authenticated
`/v1/instances/{instanceId}/call` endpoint with `window.screenshot`, then
sends the received pixels to Blockade. It returns the unmodified
`blockade.observation/v1alpha1` response beneath a
`cymonkey.observation/v1alpha1` envelope containing the instance, capture
action, and timestamp. Jangolova does not import, configure, or call Blockade.

## Model integration

External systems may combine Blockade with:

- a reasoning model that chooses tools;
- local vision models such as YOLO, SAM, and OCR;
- ONNX Runtime execution providers such as TensorRT, CUDA, OpenVINO, Core ML,
  and CPU;
- browser-local WebLLM vision-language models accelerated through WebGPU;
- cloud-provider and multimodal adapters owned and run by Blockade.

The maintained local engines are an Ultralytics YOLO/SAM worker and native
ONNX Runtime inference. ONNX execution providers change where the same model
runs without changing the Blockade observation contract. Future local engines
and hosted-provider adapters can do the same.

## Provider-adapter contract

Provider-enabled Blockade builds register adapter factories by kind. The stock
binary registers the browser-local `webllm` adapter; hosted cloud providers
remain separately registered adapters. Every registered adapter
implements the Blockade-owned `blockade.provider-adapter/v1alpha1` boundary:

- versioned observe request/response envelopes containing the public
  `blockade.observation/v1alpha1` types;
- capability and readiness reporting;
- typed authentication, rate-limit, timeout, cancellation, unavailable, and
  invalid-response failures;
- concurrent calls with deterministic close behavior.

Blockade wraps adapter calls with a configurable timeout, enforces a bounded
image-and-prompt payload, preserves request IDs and image bytes, and validates every
returned observation before it reaches a caller. Its HTTP service reports the
selected backend's actual capabilities and retains typed adapter error kinds.

Provider adapters are declared separately from local engines but share the
same inference-ID namespace:

```yaml
apiVersion: blockade.config/v1alpha1
providerAdapters:
  - id: hosted-vision
    kind: registered-provider-kind
    timeout: 30s
    maxPayloadBytes: 8388608
    settings:
      model: provider-model-name
    secrets:
      apiToken:
        env: BLOCKADE_HOSTED_VISION_TOKEN
```

Only environment references are accepted under `secrets`; plaintext secret
fields and credential-like settings are rejected. Use `--inference
hosted-vision` with `cymonkey blockade observe` or `cymonkey blockade serve`. The legacy
`--engine` flag remains an alias during migration.

The public and provider-adapter schemas are in `lib/blockade/protocol/v1alpha1/`.
The local worker implementation is in `infra/deploy/blockade/`.

## WebLLM/WebGPU backend

`kind: webllm` runs a vision-language model in a dedicated Chromium process
owned by Blockade. The process is an inference runtime only: it never attaches
to a caller's browser, receives a target identifier, or performs an action.
It uses WebLLM's dedicated-worker API, sends the supplied screenshot as an
OpenAI-style image message, constrains generation to Blockade's observation
shape, and returns the same `blockade.observation/v1alpha1` response as every
other backend.

```yaml
apiVersion: blockade.config/v1alpha1
providerAdapters:
  - id: browser-vlm
    kind: webllm
    timeout: 2m
    maxPayloadBytes: 8388608
    settings:
      model: Phi-3.5-vision-instruct-q4f16_1-MLC
      maxTokens: "1024"
      contextWindowSize: "6144"
      headless: "true"
```

Blockade locates Google Chrome or Chromium automatically. Set
`browserExecutable` when it is installed at a non-standard path. WebLLM and
model artifacts are fetched on first startup and retained in a Blockade-owned
browser profile under the operating system user-cache directory; set
`cacheDirectory` for a deployment-managed persistent volume. The pinned
WebLLM module defaults to `https://esm.run/@mlc-ai/web-llm@0.2.84`; a trusted
HTTPS mirror can be selected with `moduleURL`.

The adapter reports `image.observe` and `vision.language`. Model loading is
asynchronous, so production startup should run `cymonkey blockade serve` and wait until
`/healthz` becomes ready before sending observations. This backend is
experimental while upstream WebLLM vision support matures. See
[`docs/blockade-webllm.md`](blockade-webllm.md) for operation and limitations.
