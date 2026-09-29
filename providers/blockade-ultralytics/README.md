# Blockade Ultralytics provider

This source package implements `blockade.provider@v1alpha1` for local YOLO
detection and SAM segmentation. It maps Ultralytics output into normalized
Blockade observations.

Blockade owns the Python runtime, starts the provider processes, distributes
requests across them, and stops them. The package supplies only its descriptor,
Python source, and dependencies.

## Package

Install a complete `python@3.12.8` distribution into Blockade first. Its
`runtime.json` lists every runtime file and digest; Blockade copies and verifies
the distribution rather than using `python3` from `PATH`.

```sh
cymonkey blockade runtime manifest --source /path/to/python-tree \
  --name python --version 3.12.8 --executable bin/python3 \
  --output /path/to/python-tree/runtime.json
```

Vendor this provider's pinned packages with that runtime so compiled wheels
match its platform and Python version:

```sh
/path/to/python3 -m pip install \
  --target providers/blockade-ultralytics/python/vendor \
  -r providers/blockade-ultralytics/python/requirements.txt
python3 providers/blockade-ultralytics/package.py \
  --version 1.0.0 --platform darwin-arm64
cymonkey blockade runtime install --manifest /path/to/python/runtime.json
cymonkey plugins install \
  --manifest providers/blockade-ultralytics/plugin.json
```

`package.py` includes `blockade.json`, the provider source, requirements, and
every vendored dependency in a digest checked source package. Its generated
manifest has this shape:

```json
{
  "apiVersion": "provider.plugin/v1alpha1",
  "name": "local-ultralytics",
  "version": "1.0.0",
  "contracts": [
    {"name": "blockade.provider", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "files": [
    {"path": "blockade.json", "sha256": "<sha256>"},
    {"path": "python/provider.py", "sha256": "<sha256>"}
  ]
}
```

`blockade.json` selects the exact managed runtime and entrypoint. Cymonkey's
generic installer sees opaque files and the `blockade.provider` contract;
Blockade interprets the descriptor.

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

Model inspection starts one short lived provider process through the same
managed runtime. It imports the vendored packages and loads both weights before
reporting ready, then exits. Long lived inference starts only afterward.

## Configuration

```yaml
apiVersion: blockade.config/v1alpha1
providers:
  - id: local-yolo-sam
    provider: local-ultralytics
    model: yolo-sam@1.0.0
    workers: 1
```

`workers` defaults to one and can be set from 1 to 32. It is Blockade
configuration because Blockade owns the processes. The provider has no
`command` or `workers` setting and cannot choose an ambient Python executable.
