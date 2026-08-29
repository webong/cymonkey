# Blockade

Blockade is a read-only visual observation subsystem used by Jangolova and
available standalone. It runs local or caller-owned pixel-oriented models and
returns normalized detections, regions, masks, and confidence. It does not
plan, interact with targets, or host an agent.

## Ownership boundary

```text
Standalone caller / provider adapter
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
pixels → Blockade engine → observations → caller decision
```

Blockade has no Cymonkey dependency. It does not know whether a supplied image
came from a browser, desktop application, renderer, file, or camera, and it
never initiates an action from an observation.

## Jangolova composition

When an external agent uses Blockade through Jangolova, Jangolova is the
context switcher:

```text
external agent
  → Jangolova attached instance
  → policy-authorized Cymonkey window.screenshot
  → pixels only
  → Blockade ObserveRequest
  → normalized Blockade ObserveResponse
  → Jangolova provenance envelope + audit events
  → external agent decides whether to request a later Cymonkey action
```

The `POST /v1/instances/{instanceId}/observe` endpoint currently captures the
negotiated `window.screenshot` action. It returns the unmodified
`blockade.observation/v1alpha1` response nested under an
`interaction.engine/v1alpha1` envelope containing the instance, capture action,
and timestamp. The direct MCP server exposes the same workflow as
`jangolova_instance_observe`.

Jangolova can compose Blockade in either deployment mode:

- `JANGOLOVA_BLOCKADE_CONFIG` starts one configured Blockade engine inside the
  Jangolova process. `JANGOLOVA_BLOCKADE_ENGINE` optionally selects its engine
  ID (otherwise the first engine is selected). Jangolova calls the engine
  directly and owns its shutdown. A local-Ultralytics engine uses Blockade's
  private stdio worker IPC; an in-process engine such as ONNX has no HTTP hop.
- `JANGOLOVA_BLOCKADE_ENDPOINT` uses a separately deployed Blockade HTTP
  service.

The variables are mutually exclusive. Without either, Blockade remains
independently usable and the Jangolova observation route reports that no
Blockade client is configured.

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
