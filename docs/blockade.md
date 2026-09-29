# Blockade

Blockade is a standalone library and service for model-backed content
observation and interpretation. Its scope covers text, images, and sound using
local or cloud models. It can turn content into evidence or meaning, such as
descriptions, classifications, transcripts, and summaries when a model supports
them. The image observation contract returns normalized detections, regions,
masks, and confidence. The separate content contract accepts text, images, and
audio and returns findings such as summaries and transcripts with input-part
provenance. A model provider supplies those interpretations; no text or audio
model is bundled. Blockade does not plan, interact with targets, host an agent,
or depend on Cymonkey or Jangolova.

## Target ownership boundary

```text
Cymonkey or standalone caller
  ├─ supplies content
  └─ decides what to do with observations
          ↓ image observation or content interpretation contract
Blockade
  ├─ discovers providers and selects one for the requested capability
  ├─ validates requests, responses, limits, and model registrations
  └─ coordinates provider and model lifecycle
          ↓ provider contract
Local or cloud provider
  ├─ loads a model or calls a hosted model service
  └─ maps native inputs and outputs to Blockade's contract
          ↓ normalized evidence or interpretation
Cymonkey or standalone caller
```

Blockade owns the inference contract, provider discovery, selection, and result
validation. Each provider owns its runtime, model loading, preprocessing, and
native request/response mapping. A provider can run a local Python worker,
ONNX Runtime, WebLLM, or an operating-system model framework, or call a cloud
service. Cymonkey exposes its coordinated observation capability and delegates
inference to Blockade without knowing which provider runs the model.

### Provider and model installation target

Provider installation supplies the executable or package that knows how to
run a model or call a service. Model installation is a separate Blockade
workflow for local providers: it will acquire or register an artifact, verify
its digest, record the files and task capabilities the provider requires, and
ask that provider to check compatibility before the model is made available.
The provider remains responsible for loading or converting those files. A
cloud provider may register a remote model identifier without downloading a
local artifact. File extensions alone cannot establish model compatibility.

Today the executable provider contract exists, but there is no Blockade model
installer. `local-ultralytics` and `onnx` are still built-in engine kinds, and
WebLLM is a bundled adapter. They should remain usable while their model and
runtime-specific behavior moves behind the same provider boundary.

```text
pixels → configured Blockade inference backend → observations → caller decision
```

Blockade has no Cymonkey dependency. It does not know whether supplied content
came from a browser, desktop application, renderer, file, microphone, or
camera, and it never initiates an action from an observation.

## Cymonkey/Jangolova composition

Cymonkey registers Blockade adapters in process for its CLI and may supervise a
Blockade HTTP service alongside its Jangolova provider. Blockade remains usable
without Cymonkey:

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

Blockade's model artifacts, model families, and inference backends are separate
choices. The current Ultralytics worker loads YOLO and SAM `.pt` weights. The
native ONNX Runtime backend loads a YOLO `.onnx` export; its CPU, CUDA,
TensorRT, OpenVINO, and Core ML execution providers select hardware rather
than model behavior. A browser-local WebLLM adapter runs a vision-language
model through WebGPU. Installed provider executables may call other local or
cloud models and map their output to Blockade's response contracts. An
external agent or application chooses what to do with those results.
The [model artifact comparison](../lib/blockade/README.md#model-artifact-formats)
distinguishes the formats Blockade loads today from formats a future provider
could use.

Text understanding, transcription, sound-event recognition, and cross-modal
interpretation use `blockade.content/v1alpha1` when a capable provider is
installed. The image observation schema remains separate.

### Interpret content through an executable provider

Install a provider executable whose manifest has `kind: blockade.provider`,
then select its installed name in `providerAdapters[].kind`. The executable
advertises `text.interpret`, `audio.interpret`, `image.interpret`, and/or
`multimodal.interpret` through `blockade.capabilities`. It implements
`blockade.interpret` for those capabilities. An image-only provider can keep
using `blockade.observe`; a content-only provider does not need that method.

For example, `blockade.yaml` can select an installed provider named
`content-model`:

```yaml
apiVersion: blockade.config/v1alpha1
providerAdapters:
  - id: content-model
    kind: content-model
```

```sh
cymonkey plugins install --manifest /path/to/content-model/plugin.json
cymonkey blockade interpret --config blockade.yaml --inference content-model \
  --text-file note.txt --prompt 'Summarize the main points'
cymonkey blockade interpret --config blockade.yaml --inference content-model \
  --audio recording.wav --audio-type audio/wav --prompt 'Transcribe speech'
```

The configured Blockade service also exposes `POST /v1/interpret`:

```json
{"apiVersion":"blockade.content/v1alpha1","requestId":"note-1","parts":[{"kind":"text","text":"Meeting starts at 9."}],"prompt":"Summarize"}
```

Findings have a kind, text, and `sourceParts` indexes into the request. Binary
image and audio parts use base64 JSON data plus a media type. The request is
limited to 16 parts and 10 MiB of decoded content; a configured adapter may
set a lower `maxPayloadBytes`. Unsupported modalities return HTTP 501. See the
[content schemas](../lib/blockade/protocol/v1alpha1/) and
[plugin protocol](cymonkey-plugins.md).

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
- optional interpret envelopes containing `blockade.content/v1alpha1` parts
  and findings, routed only to advertised modalities;
- capability and readiness reporting;
- typed authentication, rate-limit, timeout, cancellation, unavailable, and
  invalid-response failures;
- concurrent calls with deterministic close behavior.

Blockade wraps adapter calls with a configurable timeout, enforces bounded
payloads, preserves request IDs and input bytes, and validates returned
observations and interpretations before they reach a caller. Its HTTP service
reports the selected backend's actual capabilities and retains typed adapter
error kinds.

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
