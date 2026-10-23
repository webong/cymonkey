# Cymonkey ONNX content plugin

This executable implements `content.provider@v1` with ONNX Runtime. Its
current model profile accepts one YOLO detection graph with artifact role
`yolo`, file extension `.onnx`, and capability `object.detect`.

The plugin owns ONNX Runtime initialization, graph inspection, image
preprocessing, inference, YOLO output decoding, non maximum suppression, and
mapping into Cymonkey observations.

## Build and package

```sh
go -C pkg/content-onnx build -o /path/to/package/provider .
```

Generate `plugin.json` beside the binary, then install it:

```sh
cymy plugins manifest --type content --name onnx --version 1.0.0 \
  --command /path/to/package/provider
cymy plugins install --manifest /path/to/package/plugin.json
```

## Model

Example `model.json` beside `yolo11n.onnx`:

```json
{
  "apiVersion": "content.model/v1",
  "name": "yolo11n",
  "version": "1.0.0",
  "plugin": "onnx",
  "capabilities": ["object.detect"],
  "artifacts": [
    {"role": "yolo", "file": "yolo11n.onnx", "sha256": "<sha256>"}
  ]
}
```

During installation the plugin opens the graph through ONNX Runtime and checks
its image input and detection output layout. The inspection runs in a short
lived plugin process before Cymonkey publishes the model version.

## Configuration

```yaml
apiVersion: cymonkey.content-config/v1alpha1
adapters:
  - id: local-yolo
    plugin: onnx
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
