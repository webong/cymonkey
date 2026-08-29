# Blockade

Blockade is Jangolova's read-only visual observation subsystem. It runs local
or caller-owned pixel-oriented models and returns normalized detections,
regions, masks, and confidence. It does not plan, interact with targets, or
host an agent.

## Ownership boundary

```text
External agent / provider adapter
  ├─ selects a model provider
  ├─ supplies credentials and provider-native mapping
  └─ decides what to do with observations
          ↓ blockade.observation/v1alpha1
Blockade
  ├─ runs local inference engines and workers
  └─ returns normalized observations
```

Blockade must not contain hardcoded cloud-provider integrations. A hosted
vision or multimodal provider is an adapter outside Blockade that maps its
native request/response format to the normalized observation contract.

```text
pixels → Blockade engine → observations → external agent decision
                                      ↘ Cymonkey action request
```

## Model integration

External systems may combine Blockade with:

- a reasoning model that chooses tools;
- vision models such as YOLO, SAM, OCR, ONNX, OpenVINO, or TensorRT;
- multimodal models that consume pixels and language.

The first maintained engine is an Ultralytics YOLO/SAM worker. Future
implementations can use another local engine or a caller-owned provider without
changing Jangolova's observation contract.

The reference contract and local worker implementation are in
`protocol/blockade/` and `deploy/blockade/`.
