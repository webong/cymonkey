# Blockade

Blockade defines stable contracts for model backed observation and content
interpretation. It accepts text, image, and audio content, selects a configured
provider, validates the result, and returns provider neutral output.

Blockade does not implement inference runtimes. ONNX Runtime, Ultralytics,
WebLLM, operating system model frameworks, and cloud APIs belong in provider
executables.

## Boundary

```text
caller
  -> Blockade request and validation
  -> selected provider executable
  -> local runtime or cloud service
  -> normalized Blockade response
```

Blockade owns:

- `blockade.observation/v1alpha1` for image evidence;
- `blockade.content/v1alpha1` for text, image, and audio interpretation;
- provider registration, selection, limits, readiness, and error mapping;
- local model installation, digest verification, inspection, lookup, and
  removal;
- the HTTP and CLI surfaces.

A provider owns:

- model format support;
- runtime initialization and hardware selection;
- preprocessing, inference, and postprocessing;
- conversion from native results to Blockade responses;
- any remote service integration.

## Models, formats, and providers

These are separate concepts:

| Concept | Examples | Meaning |
| --- | --- | --- |
| Model family | YOLO, SAM, a speech model, a vision language model | The trained behavior and architecture. |
| Artifact format | `.pt`, `.onnx`, `.gguf`, `.safetensors`, `.mlpackage` | How model data is stored. |
| Provider | `onnx`, `local-ultralytics`, `webllm`, a cloud adapter | The executable that can load or call the model and speak Blockade's protocol. |
| Hardware backend | CPU, CUDA, TensorRT, OpenVINO, Core ML, WebGPU | Where a provider executes supported work. |

Blockade does not infer compatibility from an extension. The selected provider
inspects a local model before Blockade installs it.

The repository currently contains these separate provider modules:

| Provider | Current scope |
| --- | --- |
| `providers/blockade-onnx` | YOLO detection from one `.onnx` artifact through ONNX Runtime. CPU, CUDA, TensorRT, OpenVINO, and Core ML execution providers are configurable. |
| `providers/blockade-ultralytics` | YOLO detection and SAM segmentation from two `.pt` artifacts through a managed Python worker. |
| `providers/blockade-webllm` | Browser local vision language observation through WebLLM and WebGPU. Its remote model ID lives in provider settings, so it does not use Blockade's local model registry. |

Other formats become supported when a provider for them is installed. A future
Core ML provider could load `.mlmodel`, `.mlpackage`, or `.mlmodelc` directly;
the existing ONNX provider's Core ML setting instead executes a compatible
`.onnx` graph through ONNX Runtime.

## Configure a provider

Every entry creates one configured provider instance. `provider` is the name
from the installed executable's `plugin.json`. `id` is the instance selected
with `--inference`.

```yaml
apiVersion: blockade.config/v1alpha1
providers:
  - id: local-yolo
    provider: onnx
    model: yolo11n@1.0.0
    timeout: 30s
    maxPayloadBytes: 8388608
    settings:
      executionProviders: '[{"name":"coreml"},{"name":"cpu"}]'
```

`settings` are provider owned and remain opaque to Blockade. Secret values are
not allowed there. Put logical secret names under `secrets` and reference an
environment variable:

```yaml
secrets:
  apiToken:
    env: BLOCKADE_PROVIDER_TOKEN
```

The provider requests a configured logical secret at runtime. Blockade resolves
the environment reference without placing the value in YAML.

## Install a local model

A local model manifest identifies the exact provider, capabilities, artifacts,
roles, and SHA-256 digests:

```json
{
  "apiVersion": "blockade.model/v1alpha1",
  "name": "yolo11n",
  "version": "1.0.0",
  "provider": "onnx",
  "capabilities": ["object.detect"],
  "artifacts": [
    {
      "role": "yolo",
      "file": "yolo11n.onnx",
      "sha256": "<64 lowercase hexadecimal characters>"
    }
  ]
}
```

Keep `model.json` and its artifacts in the same source directory. Install the
provider executable first, configure a matching provider instance, and then
install the model:

```sh
cymonkey plugins install --manifest /providers/onnx/plugin.json
cymonkey blockade model install \
  --manifest /models/yolo11n/model.json \
  --config /config/blockade.yaml
cymonkey blockade model list
cymonkey blockade model remove --provider onnx --model yolo11n@1.0.0
```

Installation copies artifacts into a staging directory, verifies their
digests, starts a short lived provider process for `blockade.model.inspect`,
rechecks the files, and atomically publishes the version. It does not start the
provider's inference runtime. Startup verifies the stored files again before
passing the installed model to the provider.

`BLOCKADE_MODEL_DIR` changes the default registry root. A configured provider
can select an absolute root with `modelRoot`.

Cloud providers normally put a remote model ID in their own settings and omit
the local `model` field.

## Requests

The HTTP service exposes:

- `GET /healthz`
- `GET /capabilities`
- `POST /v1/observe`
- `POST /v1/interpret`

Image observation returns detections, regions, masks, confidence, and
provenance. Content interpretation returns findings linked to their input part
indexes. Providers advertise the capabilities they implement, including
`image.observe`, `object.detect`, `object.segment`, `text.interpret`,
`audio.interpret`, `image.interpret`, and `multimodal.interpret`.

```sh
cymonkey blockade observe --config blockade.yaml --inference local-yolo \
  --image screenshot.png --prompt 'find visible controls'
cymonkey blockade interpret --config blockade.yaml --inference content-model \
  --audio recording.wav --audio-type audio/wav --prompt 'transcribe speech'
```

The public schemas are in [`protocol/v1alpha1/`](protocol/v1alpha1/). Provider
packaging and process transport are documented in
[`docs/cymonkey-plugins.md`](../../docs/cymonkey-plugins.md).
