# Blockade

Blockade is a standalone Go module and visual inference service for AI systems.
Give it an image and an optional prompt; it returns normalized visual
evidence—objects, regions, masks, and confidence—without controlling the source
application or making decisions on the caller's behalf.

## Capabilities

- Local YOLO and SAM inference through managed Ultralytics workers.
- Native ONNX Runtime YOLO detection with ordered CPU, CUDA, TensorRT,
  OpenVINO, and Core ML execution providers.
- Blockade-owned provider-adapter registry for hosted vision and VLM backends.
- Versioned adapter envelopes, typed failures, readiness, capabilities, and
  bounded request execution.
- A provider-neutral observation contract for detections, segmentation masks,
  regions, confidence, and provenance.
- Small standalone HTTP service with health and capability endpoints.
- YAML model and engine configuration with local model-file validation.

## Quick start

```bash
go build -o .cache/bin/cymonkey ./src
.cache/bin/cymonkey blockade serve \
  --config infra/deploy/blockade/blockade.example.yaml \
  --bind 127.0.0.1:8091
```

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

The module namespace is `blockade`; Cymonkey consumes it through `blockade` and
`blockade/...` imports.

Blockade operates on pixels only. It does not know whether an image came from a
browser, camera, file, game engine, or desktop capture. It does not request
screen captures, control a target, or infer the next action. Its sole job is to
produce reliable, normalized visual evidence for the caller.

## Documentation

- [Observation contract and architecture](../../docs/blockade.md)
- [Deployment guide](../../infra/deploy/blockade/README.md)
- [Model cache](../../infra/deploy/blockade/models/README.md)
