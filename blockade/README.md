# Blockade

Blockade is a visual inference service for AI systems. Give it an image and an
optional prompt; it returns normalized visual evidence—objects, regions, masks,
and confidence—without controlling the source application or making decisions
on the caller's behalf.

## Capabilities

- Local YOLO and SAM inference through managed Ultralytics workers.
- Native ONNX Runtime YOLO detection with ordered CPU, CUDA, TensorRT,
  OpenVINO, and Core ML execution providers.
- A provider-neutral observation contract for detections, segmentation masks,
  regions, confidence, and provenance.
- Small standalone HTTP service with health and capability endpoints.
- YAML model and engine configuration with local model-file validation.

## Quick start

```bash
go build -o .cache/bin/blockade ./cmd/blockade
.cache/bin/blockade serve \
  --config infra/deploy/blockade/blockade.example.yaml \
  --bind 127.0.0.1:8091
```

The service exposes:

- `GET /healthz`
- `GET /capabilities`
- `POST /v1/observe`

Send a base64-encoded image to `POST /v1/observe`; Blockade returns a
`blockade.observation/v1alpha1` response.

## Engines

Blockade supports two local execution paths today:

- **Ultralytics workers** for YOLO detection and SAM segmentation.
- **ONNX Runtime** for native YOLO detection in the Blockade process.

The engine configuration selects model files, workers, and execution mode.
ONNX Runtime providers select the hardware backend without becoming separate
Blockade engines; CPU remains the portable fallback. Additional local engines
can map results into the same observation contract. Hosted vision APIs and VLM
adapters remain outside Blockade and are routed through Grimlock.

## Operating model

Blockade operates on pixels only. It does not know whether an image came from a
browser, camera, file, game engine, or desktop capture. It does not request
screen captures, control a target, or infer the next action. Its sole job is to
produce reliable, normalized visual evidence for the caller.

## Documentation

- [Observation contract and architecture](../docs/blockade.md)
- [Deployment guide](../infra/deploy/blockade/README.md)
- [Model cache](../infra/deploy/blockade/models/README.md)
