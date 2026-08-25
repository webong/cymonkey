# Blockade work handoff

This is the restart point for the next Blockade implementation task. Blockade
is Jangolova's observation subsystem: it runs vision models and returns
normalized observations. Cymonkey and Pacman remain responsible for
interaction and presentation; Grimlock remains the model registry and agent
orchestrator.

## Core ownership rule

**Blockade owns engines and observations. Grimlock owns provider registration,
credentials, provider-specific request mapping, and provider switching.**

This boundary must be preserved when adding ONNX, VLM, fal.ai, or any other
backend. Blockade exposes stable normalized observation types; Grimlock adapts
provider-native APIs to those types and selects the active provider.

## Current status

Implemented:

- `blockade.observation/v1alpha1` normalized observation contract.
- Go Blockade client in `internal/blockade`.
- Managed local worker pool using newline-delimited JSON over stdin/stdout.
- Python Ultralytics worker loading YOLO and SAM once per worker.
- Worker lifecycle ownership, round-robin dispatch, shutdown, and basic crash
  recovery on the next request.
- YAML configuration with `blockade.config/v1alpha1`.
- Local model attachment fields: `yoloModel`, `samModel`, `command`, and
  `workers`.
- Model-file validation through `Config.ValidateModelFiles`.
- Reproducible model-cache fixture launcher with read-only weight mounts.
- `jangolova blockade validate` and `jangolova blockade observe` commands.
- Ultralytics inference adapter `deploy/blockade/app.py`: library chatter is
  now kept off stdout (fresh-environment settings banners broke NDJSON
  framing), and worker stderr is surfaced for diagnosis.
- Gated real-inference smoke test `TestLocalUltralyticsFixtureSmoke`
  (env `JANGOLOVA_BLOCKADE_SMOKE=1`) over the checked-in
  `internal/blockade/testdata/smoke.png`, verified against pinned
  `yolo11n.pt` and `sam2_b.pt` weights in `.cache/blockade/models`.
- Grimlock read-only `blockade_observe` tool.
- Native ONNX Runtime engine (`internal/blockade/onnx.go`) behind the shared
  `Engine` interface: letterbox preprocessing, threshold plus NMS
  postprocessing, and coordinate mapping back to source pixels. Gated smoke
  test via `JANGOLOVA_BLOCKADE_SMOKE=1`,
  `JANGOLOVA_BLOCKADE_ONNX_MODEL`, and
  `JANGOLOVA_BLOCKADE_ONNXRUNTIME_LIB`. Verified against an exported
  `yolo11n.onnx` (bus.jpg: bus + four persons). ONNX engines do not support
  `samModel` yet.
- Role-aware Grimlock profiles: `reasoning`, `vision`, and `multimodal`.
- Provider-neutral Grimlock vision-provider registry seam. Cloud adapters such
  as fal.ai belong in Grimlock, not in Blockade.

Important files:

- Contract: `protocol/blockade/v1alpha1/observation.schema.json`
- Go package: `internal/blockade`
- Worker: `deploy/blockade/worker.py`
- YAML example: `deploy/blockade/blockade.example.yaml`
- Grimlock integration: `internal/grimlock/blockade.go`
- Design notes: `docs/blockade.md`

## Local test flow

```sh
go test ./internal/blockade ./internal/grimlock
go run ./cmd/jangolova blockade validate \
  --config deploy/blockade/blockade.example.yaml
JANGOLOVA_BLOCKADE_SMOKE=1 \
JANGOLOVA_BLOCKADE_PYTHON="$PWD/.cache/blockade/venv/bin/python3" \
  go test ./internal/blockade -run TestLocalUltralyticsFixtureSmoke -v
```

The smoke test skips unless weights, a Python interpreter, and
`JANGOLOVA_BLOCKADE_SMOKE=1` are present. The venv is created with:

```sh
python3 -m venv .cache/blockade/venv
.cache/blockade/venv/bin/pip install -r deploy/blockade/requirements.txt
```

For the real local fixture, place `yolo11n.pt` and `sam2_b.pt` under
`.cache/blockade/models` and run `deploy/blockade/run-fixture.sh`. The model
cache is intentionally outside Git.

The example YAML points at `/models/yolo11n.pt` and `/models/sam2_b.pt`; mount
those files or replace the paths before using `--check-files` or `observe`.

## Next tasks

1. Add the first Grimlock-owned cloud adapter (fal.ai or another selected
   provider) using credential and TLS resolution.
2. Add configurable cloud queue/submit behavior and request timeouts.
3. Add model-specific response mappers so cloud segmentation/detection output
   becomes `Observation`, `Region`, and mask data rather than raw evidence.
4. Register vision and multimodal providers explicitly in Grimlock discovery,
   alongside reasoning connectors, without exposing credentials.
5. Add screenshot capture adapters for Cymonkey/Pacman/display targets and pass
   those pixels to Blockade.
6. Add a Grimlock end-to-end test proving:

   ```text
   target screenshot → Blockade → observation → Grimlock context → approved action
   ```

## Architecture decisions

- Blockade owns inference engines, not presentation or interaction.
- Local Ultralytics runs in subprocesses, not embedded CPython via cgo.
- cgo is reserved for native engines such as ONNX Runtime where appropriate.
- YAML contains local engine model paths only. Cloud provider endpoints,
  models, and opaque credential references belong to Grimlock provider profiles;
  secrets must be resolved by the host/runtime.
- Vision models do not need to understand the Blockade protocol. Adapters map
  native model inputs/outputs to the Blockade contract.
- VLMs are `multimodal` providers: they may receive pixels and language, but
  they still return Blockade observations before Grimlock chooses actions.

## Handoff instruction

When starting a new task, read this file first, then inspect the current
working tree because this repository may contain unrelated concurrent changes.
Do not reset or overwrite those changes. Continue with the numbered next task
that the user selects, beginning with tests and contract updates before adding
another backend.
