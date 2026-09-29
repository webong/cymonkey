# Blockade

Blockade is the standalone model-backed content observation and interpretation
module for AI systems. Its scope includes text, images, and sound through local
and cloud models. Depending on the model and capability, interpretation may
produce structured evidence, descriptions, classifications, transcripts, or
summaries. The original image request accepts an image and optional prompt,
then returns normalized visual evidence—objects, regions, masks, and confidence.
The separate `blockade.content/v1alpha1` contract accepts text, image, and audio
parts and returns findings linked to their source parts. A capable provider
executable supplies the model; Blockade does not bundle a text or audio model.
Blockade does not control the source application or decide what to do with an
inference result.

## How inference is configured

Blockade accepts content, selects a configured backend, and validates its
normalized output. A provider owns model loading and maps its native output to
the Blockade response contract. The configuration has distinct layers:

| Layer | Current examples | Role |
| --- | --- | --- |
| Model family | YOLO, SAM, vision-language models | Defines what a trained model can do. YOLO and SAM are model families, not file formats or inference engines. |
| Model artifact | `.pt` weights, `.onnx` export | Stores trained model data. A file extension alone does not tell Blockade what task or output shape a model supports. |
| Inference backend | Ultralytics worker, ONNX Runtime, browser-local WebLLM, installed provider executable | Loads or calls the model and maps its output to Blockade's contract. |
| Hardware execution provider | ONNX Runtime CPU, CUDA, TensorRT, OpenVINO, Core ML | Selects where ONNX Runtime executes supported operations. |

### Model artifact formats

| Format | Best used for | Analogy | Blockade today |
| --- | --- | --- | --- |
| [`.pt` / `.pth` (PyTorch)](https://docs.pytorch.org/tutorials/beginner/basics/saveloadrun_tutorial.html) | PyTorch checkpoints and weights, including training and fine-tuning workflows. | A native project file that needs compatible PyTorch model code. | The Ultralytics worker loads the configured YOLO and SAM `.pt` models; there is no generic `.pth` loader. |
| [`.onnx` (ONNX)](https://onnx.ai/onnx/repo-docs/IR.html) | Exporting a computation graph to compatible inference runtimes and hardware. | An interchange package that a compatible runtime can execute. | The native ONNX Runtime backend runs a YOLO detection export only. |
| [`.gguf` (GGUF)](https://github.com/ggml-org/ggml/blob/master/docs/gguf.md) | Local inference with GGML-compatible runtimes, often using quantized models. | A model package prepared for a particular family of runtimes. | No built-in loader; an installed provider executable could use a GGUF runtime. |
| [`.safetensors` (Safetensors)](https://huggingface.co/docs/safetensors/index) | Storing and sharing tensor weights without pickle-based loading. | A sealed box of weights; model code and a runtime are still needed. | No built-in loader; an installed provider executable could use a compatible runtime. |
| [`.mlmodel` / `.mlpackage` / `.mlmodelc` (Core ML)](https://developer.apple.com/documentation/coreml/analyzing-a-core-ml-model-s-performance-in-xcode) | Deploying a model through Apple's Core ML framework; `.mlmodelc` is the compiled form. | An Apple-native model package, compiled for loading on a device. | No native Core ML model loader; a macOS provider executable could add one. ONNX Runtime can use its Core ML execution provider for a compatible `.onnx` model. |

GGUF succeeds earlier GGML-family *file formats*; GGML is still the name of a
model-computation library. None of these extensions alone tells Blockade which
task a model performs or guarantees that a backend can run it. Core ML model
files are distinct from ONNX Runtime's Core ML execution provider.

The Ultralytics worker currently loads YOLO and SAM `.pt` weights for image
detection and segmentation. The native ONNX Runtime backend currently runs a
YOLO `.onnx` model for detection; it does not run SAM. WebLLM is a separate
browser-local vision-language backend. Text and audio interpretation require a
capable installed provider executable.

## Capabilities

- Local image inference through managed Ultralytics workers or native ONNX
  Runtime, with the model support described above.
- Blockade-owned provider-adapter registry for browser-local WebLLM and
  separately supplied model providers.
- Versioned adapter envelopes, typed failures, readiness, capabilities, and
  bounded request execution.
- A provider-neutral observation contract for detections, segmentation masks,
  regions, confidence, and provenance.
- Small standalone HTTP service with health and capability endpoints.
- YAML model and engine configuration with local model-file validation.

## Use the library

Import `blockade` and `blockade/cli` in a Go application. The application
supplies model configuration and any separately registered provider adapters.
Run `go test ./...` from this directory to verify the module.

The service exposes:

- `GET /healthz`
- `GET /capabilities`
- `POST /v1/observe`
- `POST /v1/interpret`

Send a base64-encoded image to `POST /v1/observe`; Blockade returns a
`blockade.observation/v1alpha1` response.

`POST /v1/interpret` accepts `blockade.content/v1alpha1` with one or more
`text`, `image`, or `audio` parts. Audio and image data are base64-encoded in
JSON, with a media type. Its response contains bounded findings such as a
summary or transcript and indexes into the input parts for provenance. A
provider advertises `text.interpret`, `audio.interpret`, `image.interpret`, or
`multimodal.interpret`; an unsupported modality returns HTTP 501.

The standalone CLI exposes the same provider-backed path:

```sh
cymonkey blockade interpret --config blockade.yaml --text 'Summarize this note'
cymonkey blockade interpret --config blockade.yaml \
  --audio recording.wav --audio-type audio/wav --prompt 'Transcribe speech'
```

## Inference backends

Blockade supports two local image execution paths today:

- **Ultralytics workers** for YOLO detection and SAM segmentation.
- **ONNX Runtime** for native YOLO detection in the Blockade process.

The inference configuration selects a local engine or a named provider adapter.
ONNX Runtime providers select the hardware backend without becoming separate
Blockade engines; CPU remains the portable fallback. Additional local engines
can map results into the same observation contract. Hosted vision and VLM
packages register Blockade-owned adapter factories and keep their native APIs
behind `blockade.provider-adapter/v1alpha1`.

Provider-adapter YAML contains only non-secret settings and environment-variable
references. The base binary intentionally registers no hosted provider yet;
the reusable boundary and fake integration tests are in place for the first
real adapter package.

The intended model-installation workflow is separate from provider
installation. Blockade will register and verify local model artifacts, then
ask the selected provider to check and load them. Cloud providers can use a
remote model identifier instead. The current CLI does not yet install models;
the built-in Ultralytics and ONNX engines still receive local model paths in
YAML. See the [provider and model installation target](../../docs/blockade.md#provider-and-model-installation-target).

## Operating model

The module namespace is `blockade`; consumers import `blockade` and
`blockade/...` without an operator dependency.

The current observation path operates on supplied pixels. It does not know
whether an image came from a browser, camera, file, game engine, or desktop
capture. Text and sound interpretation use the separate content contract and
an installed model provider. Blockade does not capture targets, control
them, or infer the next action; it returns normalized inference results for
the caller.

The public schemas are in [`protocol/v1alpha1/`](protocol/v1alpha1/).
