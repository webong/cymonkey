# Blockade

Blockade is the standalone inference-interface module and service for AI
systems. Its scope covers image and sound inference through local and cloud
backends. The implemented public request today accepts an image and optional
prompt, then returns normalized visual evidence—objects, regions, masks, and
confidence. Sound inputs and outputs do not yet have a public contract.
Blockade does not control the source application or decide what to do with an
inference result.

## Capabilities

- Local YOLO and SAM inference through managed Ultralytics workers.
- Native ONNX Runtime YOLO detection with ordered CPU, CUDA, TensorRT,
  OpenVINO, and Core ML execution providers.
- Blockade-owned provider-adapter registry for separately supplied cloud vision
  and VLM backends.
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

Send a base64-encoded image to `POST /v1/observe`; Blockade returns a
`blockade.observation/v1alpha1` response.

## Inference backends

Blockade supports two local execution paths today:

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

## Operating model

The module namespace is `blockade`; consumers import `blockade` and
`blockade/...` without an operator dependency.

The current observation path operates on supplied pixels. It does not know
whether an image came from a browser, camera, file, game engine, or desktop
capture. Future sound inference belongs behind the same Blockade boundary,
with its own explicit contract. Blockade does not capture targets, control
them, or infer the next action; it returns normalized inference results for
the caller.

The public schemas are in [`protocol/v1alpha1/`](protocol/v1alpha1/).
