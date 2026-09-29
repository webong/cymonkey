# Blockade ONNX provider

This executable implements `blockade.provider@v1alpha1` with ONNX Runtime. Its
current model profile accepts one YOLO detection graph with artifact role
`yolo`, file extension `.onnx`, and capability `object.detect`.

The provider owns ONNX Runtime initialization, graph inspection, image
preprocessing, inference, YOLO output decoding, non maximum suppression, and
mapping into Blockade observations.

## Build and package

```sh
go build -o /path/to/package/provider ./providers/blockade-onnx
```

Create `/path/to/package/plugin.json`:

```json
{
  "apiVersion": "provider.plugin/v1alpha1",
  "name": "onnx",
  "version": "1.0.0",
  "contracts": [
    {"name": "blockade.provider", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "command": "provider",
  "sha256": "<sha256 of provider>"
}
```

Use the actual host platform and executable digest, then install it:

```sh
cymonkey plugins install --manifest /path/to/package/plugin.json
```

## Model

Example `model.json` beside `yolo11n.onnx`:

```json
{
  "apiVersion": "blockade.model/v1alpha1",
  "name": "yolo11n",
  "version": "1.0.0",
  "provider": "onnx",
  "capabilities": ["object.detect"],
  "artifacts": [
    {"role": "yolo", "file": "yolo11n.onnx", "sha256": "<sha256>"}
  ]
}
```

During installation the provider opens the graph through ONNX Runtime and
checks its image input and detection output layout. The inspection runs in a
short lived provider process before Blockade publishes the model version.

## Configuration

```yaml
apiVersion: blockade.config/v1alpha1
providers:
  - id: local-yolo
    provider: onnx
    model: yolo11n@1.0.0
    settings:
      runtimeLibrary: /absolute/path/to/libonnxruntime.dylib
      executionProviders: '[{"name":"coreml"},{"name":"cpu"}]'
      confidenceThreshold: "0.25"
      nmsThreshold: "0.45"
```

`runtimeLibrary` is optional when ONNX Runtime can be located normally. The
ordered `executionProviders` setting accepts `cuda`, `tensorrt`, `openvino`,
`coreml`, and `cpu`. CPU is added as the fallback when omitted. The selected
ONNX Runtime library must include every configured accelerator.

Core ML here is an ONNX Runtime execution provider for a compatible `.onnx`
graph. It does not load `.mlmodel`, `.mlpackage`, or `.mlmodelc` artifacts.
