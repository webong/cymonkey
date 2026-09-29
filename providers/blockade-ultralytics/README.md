# Blockade Ultralytics provider

This executable implements `blockade.provider@v1alpha1` for local YOLO
detection and SAM segmentation. It owns a managed pool of Python workers and
maps their output into Blockade observations.

The Python bridge is embedded in the Go executable. The host needs `python3`
on `PATH` with the packages pinned in
[`python/requirements.txt`](python/requirements.txt):

```sh
python3 -m pip install -r providers/blockade-ultralytics/python/requirements.txt
go build -o /path/to/package/provider ./providers/blockade-ultralytics
```

Create `plugin.json` beside the binary:

```json
{
  "apiVersion": "provider.plugin/v1alpha1",
  "name": "local-ultralytics",
  "version": "1.0.0",
  "contracts": [
    {"name": "blockade.provider", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "command": "provider",
  "sha256": "<sha256 of provider>"
}
```

Use the actual host platform and digest, then install the package.

## Model

The model bundle requires exactly one `yolo` `.pt` artifact, one `sam` `.pt`
artifact, and both capabilities:

```json
{
  "apiVersion": "blockade.model/v1alpha1",
  "name": "yolo-sam",
  "version": "1.0.0",
  "provider": "local-ultralytics",
  "capabilities": ["object.detect", "object.segment"],
  "artifacts": [
    {"role": "yolo", "file": "yolo11n.pt", "sha256": "<sha256>"},
    {"role": "sam", "file": "sam2_b.pt", "sha256": "<sha256>"}
  ]
}
```

Model inspection starts a temporary embedded Python worker. The worker imports
the pinned runtime packages and loads both weights before reporting ready. It
then exits; this is separate from the long lived inference startup.

## Configuration

```yaml
apiVersion: blockade.config/v1alpha1
providers:
  - id: local-yolo-sam
    provider: local-ultralytics
    model: yolo-sam@1.0.0
    settings:
      workers: "1"
```

`workers` defaults to one. `command` can override the embedded worker with a
JSON string array, for example `["python3","/opt/provider/worker.py"]`. This
override is useful for provider development; ordinary
packages should use the embedded bridge.
