# Blockade YOLO/SAM worker

This is the first local Blockade provider. It observes encoded images and
returns normalized detections and segmentation masks. Blockade owns pixel
observation; Jangolova remains responsible for interaction while an external
agent owns orchestration.

Local engine attachments can be declared in
[blockade.example.yaml](blockade.example.yaml). Use `local-ultralytics` for
managed subprocess workers and `onnx` for native ONNX Runtime integration.
Cloud providers are caller-owned adapters and are deliberately not named by
Blockade configuration.

Build and run it from the repository root:

```sh
docker build -f deploy/blockade/Containerfile -t jangolova/blockade:yolo-sam .
docker run --rm -p 127.0.0.1:8091:8091 jangolova/blockade:yolo-sam
```

For a reproducible local fixture with read-only model mounts:

```sh
mkdir -p .cache/blockade/models
# Place yolo11n.pt and sam2_b.pt in .cache/blockade/models.
BLOCKADE_MODEL_CACHE="$PWD/.cache/blockade/models" \
  deploy/blockade/run-fixture.sh
```

The launcher fails before starting Docker when either weight file is missing.
See [models/README.md](models/README.md) for the cache contract.

## Inference smoke test

A gated Go test runs real inference through the managed worker pool using a
checked-in input image (`internal/blockade/testdata/smoke.png`). It skips
unless weights, a Python interpreter, and `JANGOLOVA_BLOCKADE_SMOKE=1` are
available:

```sh
JANGOLOVA_BLOCKADE_SMOKE=1 \
JANGOLOVA_BLOCKADE_PYTHON="$PWD/.cache/blockade/venv/bin/python3" \
  go test ./internal/blockade -run TestLocalUltralyticsFixtureSmoke -v
```

Override `BLOCKADE_YOLO_MODEL_FILE` or `BLOCKADE_SAM_MODEL_FILE` when testing
alternative weights. The test asserts contract-valid observations and verifies
that undecodable images fail cleanly instead of crashing the worker.

## Native ONNX Runtime engine

Engines with `kind: onnx` run YOLO detection in-process through ONNX Runtime
(cgo) instead of managed Python workers. Export a model with Ultralytics and
point the runtime at the shared library:

```sh
python3 -m venv .cache/blockade/venv
.cache/blockade/venv/bin/pip install ultralytics onnx onnxslim
.cache/blockade/venv/bin/yolo export \
  model=.cache/blockade/models/yolo11n.pt format=onnx imgsz=640 opset=17

JANGOLOVA_BLOCKADE_ONNXRUNTIME_LIB="$PWD/.cache/blockade/libonnxruntime.dylib" \
JANGOLOVA_BLOCKADE_SMOKE=1 \
JANGOLOVA_BLOCKADE_ONNX_MODEL="$PWD/.cache/blockade/models/yolo11n.onnx" \
  go test ./internal/blockade -run TestOnnxEngineFixtureSmoke -v
```

The engine reads input and output names plus tensor layouts from the model,
letterboxes to the configured input size, runs thresholding and NMS, and maps
boxes back to original pixel coordinates. Segmentation (`samModel`) stays with
local workers for now; ONNX engines require `yoloModel` only.

The first startup downloads the configured model weights unless they are
provided through a mounted cache. Override `BLOCKADE_YOLO_MODEL` and
`BLOCKADE_SAM_MODEL` when selecting different compatible Ultralytics weights.

`POST /v1/observe` accepts JSON with a base64-encoded `image` and returns the
`blockade.observation/v1alpha1` response. The Go client is in
`internal/blockade`. An external agent or application can call this endpoint
directly, then request an allowed Jangolova action through the Engine Provider
HTTP or MCP surface.

To select an engine from YAML:

```sh
export JANGOLOVA_BLOCKADE_CONFIG=deploy/blockade/blockade.example.yaml
export JANGOLOVA_BLOCKADE_ENGINE=local-yolo-sam
```

If `JANGOLOVA_BLOCKADE_ENGINE` is omitted, the first configured engine is used.

## Managed subprocess mode

Blockade can own the Python process through stdin/stdout framed JSON IPC,
avoiding a local HTTP hop while preserving process isolation:

```go
pool, err := blockade.NewWorkerPool(ctx, blockade.WorkerConfig{
    Command: []string{"python3", "deploy/blockade/worker.py"},
    Workers: 1,
})
defer pool.Close()
client := blockade.Client{WorkerPool: pool}
```

The worker loads YOLO and SAM once, processes one request at a time, and emits
one JSON response per input line. Increase `Workers` only when the hardware
can hold multiple model copies.
