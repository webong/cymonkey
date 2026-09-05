# Blockade YOLO/SAM worker

This is the first local Blockade provider. It observes encoded images and
returns normalized detections and segmentation masks. Blockade owns pixel
observation; Jangolova remains responsible for interaction while an external
agent owns orchestration.

Local engine attachments can be declared in
[blockade.example.yaml](blockade.example.yaml). Use `local-ultralytics` for
managed subprocess workers and `onnx` for native ONNX Runtime integration.
Cloud providers and VLMs are configured as Blockade provider adapters, separate
from its local engine configuration.

Build and run it from the repository root:

```sh
docker build -f infra/deploy/blockade/Containerfile \
  -t jangolova/blockade:yolo-sam infra/deploy/blockade
docker run --rm -p 127.0.0.1:8091:8091 jangolova/blockade:yolo-sam
```

Blockade also has a standalone Go executable for the same contract. It can
run a configured local engine directly and expose the HTTP service without the
Python FastAPI container:

```sh
go build -o .cache/bin/blockade ./cmd/blockade
.cache/bin/blockade serve \
  --config infra/deploy/blockade/blockade.example.yaml \
  --bind 127.0.0.1:8091
```

For a reproducible local fixture with read-only model mounts:

```sh
mkdir -p .cache/blockade/models
# Place yolo11n.pt and sam2_b.pt in .cache/blockade/models.
BLOCKADE_MODEL_CACHE="$PWD/.cache/blockade/models" \
  infra/deploy/blockade/run-fixture.sh
```

The launcher fails before starting Docker when either weight file is missing.
See [models/README.md](models/README.md) for the cache contract.

## Inference smoke test

A gated Go test runs real inference through the managed worker pool using a
checked-in input image (`internal/blockade/testdata/smoke.png`). It skips
unless weights, a Python interpreter, and `BLOCKADE_SMOKE=1` are
available:

```sh
BLOCKADE_SMOKE=1 \
BLOCKADE_PYTHON="$PWD/.cache/blockade/venv/bin/python3" \
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

BLOCKADE_ONNXRUNTIME_LIB="$PWD/.cache/blockade/libonnxruntime.dylib" \
BLOCKADE_SMOKE=1 \
BLOCKADE_ONNX_MODEL="$PWD/.cache/blockade/models/yolo11n.onnx" \
  go test ./internal/blockade -run TestOnnxEngineFixtureSmoke -v
```

Set `BLOCKADE_ONNX_EXECUTION_PROVIDERS` to a JSON array matching the YAML
provider entries to exercise an accelerator in the same smoke test. For
example, on Apple Silicon:

```sh
mkdir -p /tmp/blockade-coreml-cache
BLOCKADE_ONNX_EXECUTION_PROVIDERS='[{"name":"coreml","options":{"ModelFormat":"MLProgram","MLComputeUnits":"ALL","RequireStaticInputShapes":"1","ModelCacheDirectory":"/tmp/blockade-coreml-cache"}},{"name":"cpu"}]' \
BLOCKADE_ONNXRUNTIME_LIB="$PWD/.cache/blockade/libonnxruntime.dylib" \
BLOCKADE_SMOKE=1 \
BLOCKADE_ONNX_MODEL="$PWD/.cache/blockade/models/yolo11n.onnx" \
  go test ./internal/blockade -run TestOnnxEngineFixtureSmoke -v
```

The engine reads input and output names plus tensor layouts from the model,
letterboxes to the configured input size, runs thresholding and NMS, and maps
boxes back to original pixel coordinates. Segmentation (`samModel`) stays with
local workers for now; ONNX engines require `yoloModel` only.

### ONNX Runtime execution providers

An ONNX engine can declare an ordered `executionProviders` list. Blockade
supports `tensorrt`, `cuda`, `openvino`, `coreml`, and `cpu`. Options are passed
unchanged to the provider's ONNX Runtime V2 configuration API. CPU is always
the implicit final fallback, so an omitted list preserves the portable
CPU-only behavior. If `cpu` is written explicitly, it must be last.

The ONNX Runtime shared library must contain every configured accelerator.
Blockade fails at startup when it cannot initialize one, which prevents a
requested accelerated deployment from silently running entirely on CPU.
Provider caches must point to a writable volume distinct from a read-only model
mount. The effective order is recorded in each detection's evidence as
`onnx:<model>;ep=<provider,...,cpu>`.

For NVIDIA, register TensorRT before CUDA so CUDA can execute nodes TensorRT
does not support:

```yaml
- id: onnx-yolo-nvidia
  kind: onnx
  yoloModel: /models/yolo11n.onnx
  executionProviders:
    - name: tensorrt
      options:
        device_id: "0"
        trt_fp16_enable: "1"
        trt_engine_cache_enable: "1"
        trt_engine_cache_path: /var/cache/blockade/tensorrt
    - name: cuda
      options:
        device_id: "0"
    - name: cpu
```

For Intel hardware, OpenVINO can choose an available device, with ONNX Runtime
CPU fallback retained for unsupported graph nodes:

```yaml
- id: onnx-yolo-openvino
  kind: onnx
  yoloModel: /models/yolo11n.onnx
  executionProviders:
    - name: openvino
      options:
        device_type: AUTO
        cache_dir: /var/cache/blockade/openvino
    - name: cpu
```

For Apple Silicon, Core ML can use all compatible compute units:

```yaml
- id: onnx-yolo-coreml
  kind: onnx
  yoloModel: /models/yolo11n.onnx
  executionProviders:
    - name: coreml
      options:
        ModelFormat: MLProgram
        MLComputeUnits: ALL
        RequireStaticInputShapes: "1"
        ModelCacheDirectory: /var/cache/blockade/coreml
    - name: cpu
```

Use the NVIDIA path first for production GPU throughput, then validate
OpenVINO for Intel deployments and Core ML for Apple deployments. Each path
still exposes the same `blockade.observation/v1alpha1` contract. Provider/VLM
APIs run through Blockade adapters rather than becoming Blockade local engines.

The first startup downloads the configured model weights unless they are
provided through a mounted cache. Override `BLOCKADE_YOLO_MODEL` and
`BLOCKADE_SAM_MODEL` when selecting different compatible Ultralytics weights.

`POST /v1/observe` accepts JSON with a base64-encoded `image` and returns the
`blockade.observation/v1alpha1` response. The Go client is in
`internal/blockade`. An external agent or application can call this endpoint
directly, then request an allowed Jangolova action through the Engine Provider
HTTP or MCP surface.

To select an engine from YAML, run Blockade directly:

```sh
blockade serve --config infra/deploy/blockade/blockade.example.yaml
```

## Hosted provider adapters

Hosted vision/VLM integrations are Blockade-owned packages registered by
adapter kind. The stock binary currently includes the reusable registry and
contract but no real hosted provider package. A provider-enabled build passes
its registry to `blockadecli.RunWithProviderAdapters`; no Cymonkey or
Jangolova change is required.

```yaml
apiVersion: blockade.config/v1alpha1
providerAdapters:
  - id: hosted-vision
    kind: registered-provider-kind
    timeout: 30s
    maxPayloadBytes: 8388608
    settings:
      model: provider-model-name
    secrets:
      apiToken:
        env: BLOCKADE_HOSTED_VISION_TOKEN
```

Start or invoke the selected backend with `--inference hosted-vision`.
`--engine` remains a compatibility alias. Adapter settings cannot use common
credential field names, and a secret declaration accepts only an environment
variable reference. Blockade resolves it lazily at runtime and never includes
the value in configuration, error messages, evidence, or HTTP responses.

The adapter boundary is `blockade.provider-adapter/v1alpha1`; its schema is
`protocol/blockade/v1alpha1/provider-adapter.schema.json`. Blockade validates
the nested observation response, enforces the configured timeout and payload
limit, and exposes the adapter's readiness and capabilities through the normal
service endpoints. Authentication, rate-limit, timeout, cancellation,
unavailable, and invalid-response errors remain machine-readable.

For coordinated browser observation, Cymonkey obtains a policy-authorized
screenshot from Jangolova and submits those pixels to this service. Jangolova
does not embed or configure Blockade.

## Managed subprocess mode

Blockade can own the Python process through stdin/stdout framed JSON IPC,
avoiding a local HTTP hop while preserving process isolation:

```go
pool, err := blockade.NewWorkerPool(ctx, blockade.WorkerConfig{
    Command: []string{"python3", "infra/deploy/blockade/worker.py"},
    Workers: 1,
})
defer pool.Close()
client := blockade.Client{WorkerPool: pool}
```

The worker loads YOLO and SAM once, processes one request at a time, and emits
one JSON response per input line. Increase `Workers` only when the hardware
can hold multiple model copies.
