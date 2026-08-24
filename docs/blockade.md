# Blockade

Blockade is Jangolova's observation subsystem. It runs pixel-oriented models
locally or through caller-owned model services and returns normalized visual
observations. It does not perform interaction or presentation.

## Ownership boundary

Blockade owns engines and observations. Grimlock owns provider registration,
credentials, provider-specific request mapping, and provider switching.

```text
Grimlock
  ├─ registers vision and multimodal providers
  ├─ resolves credentials and TLS
  ├─ maps provider-native requests and responses
  └─ selects or switches providers
          ↓ provider-neutral Blockade contract
Blockade
  ├─ runs local inference engines
  ├─ manages local workers
  └─ returns normalized observations
```

Blockade must not contain hardcoded cloud-provider integrations. A provider
such as fal.ai, another hosted service, or a caller-owned VLM is implemented
behind Grimlock's provider interface. Switching providers must not require a
change to Blockade's observation contract or local engine implementations.

```text
pixels → Blockade engine → observations → Grimlock → approved action
                                      ↘ Cymonkey / Pacman
```

## Model registration

Grimlock is the model registry and orchestration boundary, but models have
roles:

- `reasoning`: text/agent models used to plan and select tools;
- `vision`: YOLO, SAM, OCR, and similar forward-pass models;
- `multimodal`: VLMs that accept pixels and language and may also reason.

A vision model does not need to understand Grimlock or Blockade protocols.
Grimlock or a Blockade provider adapter translates the normalized
`blockade.observation/v1alpha1` request into the model's native API. This keeps
YOLO, SAM, ONNX, OpenVINO, TensorRT, and VLM implementations replaceable.

The intended flow is:

```text
Grimlock model registry
  ├─ reasoning model → agent loop
  ├─ vision model    → Blockade adapter → detections/masks
  └─ multimodal      → Blockade adapter → grounded observations/reasoning
```

The first implementation uses an external Ultralytics YOLO/SAM worker. Future
Blockade backends can use ONNX Runtime, OpenVINO, TensorRT, or a VLM gateway
without changing Grimlock's observation tool.

Blockade must not contain provider-specific cloud code. A cloud service such as
fal.ai is a Grimlock-registered `VisionProvider`; Grimlock resolves its
credentials and maps its native request/response format to Blockade's types.

For the maintained implementation status and resumable task list, see
[Blockade work handoff](blockade-handoff.md).
