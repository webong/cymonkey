# Blockade

Blockade is a standalone read-only visual-inference service. It runs local
pixel-oriented models and returns normalized detections, regions, masks, and
confidence. It does not plan, interact with targets, host an agent, or depend
on Cymonkey or Jangolova.

## Ownership boundary

```text
Cymonkey or standalone caller
  ├─ supplies pixels
  ├─ selects a configured local engine
  └─ decides what to do with observations
          ↓ blockade.observation/v1alpha1
Blockade
  ├─ runs local inference engines and workers
  └─ returns normalized observations

Grimlock (separate integration path)
  ├─ owns hosted-provider and VLM credentials
  └─ maps provider-native results to normalized observations
```

Blockade must not contain hardcoded cloud-provider or VLM integrations.
Grimlock owns those integrations, their credentials, and provider-native
request/response mapping. Cymonkey registers its coordinated observation
capability with Grimlock; Blockade remains a local inference service behind
that capability.

```text
pixels → Blockade engine → observations → caller decision
```

Blockade has no Cymonkey dependency. It does not know whether a supplied image
came from a browser, desktop application, renderer, file, or camera, and it
never initiates an action from an observation.

## Cymonkey/Jangolova composition

Cymonkey may supervise the standalone `blockade` executable alongside the
standalone `jangolova` tool server. Blockade remains usable without Cymonkey:

```sh
blockade serve --config infra/deploy/blockade/blockade.example.yaml
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
- provider and multimodal integrations routed through Grimlock.

The maintained local engines are an Ultralytics YOLO/SAM worker and native
ONNX Runtime inference. ONNX execution providers change where the same model
runs without changing the Blockade observation contract. Future local engines
can do the same; hosted inference remains outside Blockade.

The reference contract and local worker implementation are in
`protocol/blockade/` and `infra/deploy/blockade/`.
