# Cymonkey Ultralytics content plugin

This source package implements `cymonkey.content@v1alpha1` for local YOLO
detection and SAM segmentation. It maps Ultralytics output into normalized
Cymonkey observations.

Cymonkey owns the Python runtime, starts the plugin processes, distributes
requests across them, and stops them. The package supplies only its descriptor,
Python source, and dependencies.

## Package

Install a complete `python@3.12.8` distribution into Cymonkey first. Its
`runtime.json` lists every runtime file and digest; Cymonkey copies and verifies
the distribution rather than using `python3` from `PATH`.

```sh
cymy content runtime manifest --source /path/to/python-tree \
  --name python --version 3.12.8 --executable bin/python3 \
  --output /path/to/python-tree/runtime.json
```

Vendor this provider's pinned packages with that runtime so compiled wheels
match its platform and Python version:

```sh
/path/to/python3 -m pip install \
  --target providers/content-ultralytics/python/vendor \
  -r providers/content-ultralytics/python/requirements.txt
python3 providers/content-ultralytics/package.py \
  --version 1.0.0 --platform darwin-arm64
cymy content runtime install --manifest /path/to/python/runtime.json
cymy plugins install \
  --manifest providers/content-ultralytics/plugin.json
```

`package.py` includes `content.json`, the plugin source, requirements, and
every vendored dependency in a digest checked source package. Its generated
manifest has this shape:

```json
{
  "apiVersion": "cymonkey.plugin/v1alpha1",
  "name": "local-ultralytics",
  "version": "1.0.0",
  "contracts": [
    {"name": "cymonkey.content", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "files": [
    {"path": "content.json", "sha256": "<sha256>"},
    {"path": "python/provider.py", "sha256": "<sha256>"}
  ]
}
```

`content.json` uses `cymonkey.content-source/v1alpha1` and selects the exact
managed runtime and entrypoint. The generic installer treats its payload as
opaque; Cymonkey's content adapter interprets the descriptor.

## Model

The model bundle requires exactly one `yolo` `.pt` artifact, one `sam` `.pt`
artifact, and both capabilities:

```json
{
  "apiVersion": "cymonkey.model/v1alpha1",
  "name": "yolo-sam",
  "version": "1.0.0",
  "plugin": "local-ultralytics",
  "capabilities": ["object.detect", "object.segment"],
  "artifacts": [
    {"role": "yolo", "file": "yolo11n.pt", "sha256": "<sha256>"},
    {"role": "sam", "file": "sam2_b.pt", "sha256": "<sha256>"}
  ]
}
```

Model inspection starts one short lived plugin process through the same
managed runtime. It imports the vendored packages and loads both weights before
reporting ready, then exits. Long lived inference starts only afterward.

## Configuration

```yaml
apiVersion: cymonkey.content-config/v1alpha1
adapters:
  - id: local-yolo-sam
    plugin: local-ultralytics
    model: yolo-sam@1.0.0
    workers: 1
```

`workers` defaults to one and can be set from 1 to 32. Cymonkey owns the worker
pool. The plugin has no
`command` or `workers` setting and cannot choose an ambient Python executable.
