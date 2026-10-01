# Blockade library

Blockade is the internal content observation and interpretation library. It
defines Go contracts for text, image, audio, and video requests, validates
provider output, selects configured adapters, and manages local model and
runtime artifacts.

Blockade is independently embeddable. An application that imports it may use
its native types and protocols. Cymonkey users work through `cymy content` and
the `cymonkey.content/*` contracts documented in
[`docs/content.md`](../../docs/content.md).

## Library boundary

Blockade owns:

- request and response validation;
- adapter registration, selection, health, limits, and error mapping;
- local model installation, digest verification, inspection, and removal;
- managed source runtime installation and process supervision;
- an optional HTTP handler for applications that need a service boundary.

An inference adapter owns:

- model format support;
- model or remote API initialization;
- preprocessing, inference, and postprocessing;
- hardware backend selection;
- conversion from its native result to Blockade's normalized response.

Blockade does not ship a command line application. Host applications decide
which commands and public wire contracts they expose.

## Models, formats, and adapters

These concepts are independent:

| Concept | Examples | Meaning |
| --- | --- | --- |
| Model family | YOLO, SAM, a speech model, a vision language model | Trained behavior and architecture |
| Artifact format | `.pt`, `.onnx`, `.gguf`, `.safetensors`, `.mlpackage` | Stored model representation |
| Adapter | ONNX Runtime, Ultralytics, WebLLM, a cloud API client | Code that loads or calls a model |
| Hardware backend | CPU, CUDA, TensorRT, OpenVINO, Core ML, WebGPU | Where supported inference executes |

Blockade does not infer compatibility from a filename extension. The selected
adapter inspects a model before installation.

## Native embedding

Create a registry, register adapters, and start a configured adapter through
the Go API:

```go
registry := blockade.NewProviderAdapterRegistry()

err := registry.Register("example", factory)
if err != nil {
    return err
}

engine, err := blockade.StartConfiguredInference(
    ctx,
    blockade.ProviderAdapterConfig{
        ID:       "local",
        Provider: "example",
    },
    registry,
    blockade.EnvironmentSecretResolver{},
)
if err != nil {
    return err
}
defer engine.Close()
```

`blockade.NewHTTPServer` exposes the native Blockade service contract as an
`http.Handler`. `blockade.RequireBearerToken` can protect that handler. The
embedding application owns process startup, flags, configuration paths, TLS,
and user facing names.

## Native plugin protocol

A standalone Blockade application may use the adapter in `plugin/` with
packages declaring `blockade.provider@v1alpha1`. Its process methods use the
`blockade.*` prefix. Source packages use `blockade.json` with
`blockade.provider-package/v1alpha1`.

The plugin adapter accepts a host supplied `Protocol`. Cymonkey uses this hook
to expose `cymonkey.content@v1alpha1`, `cymonkey.content.*` methods, and a
`content.json` descriptor without teaching plugin authors about Blockade.

## Runtime ownership

For source adapters, the registry stores a complete digest checked runtime.
Blockade selects its executable and supervises worker processes. A source
package supplies its entrypoint and dependencies but cannot select an ambient
Python installation.

Runtime and model stores reject symbolic links, verify files before use, and
publish installations atomically. Cloud adapters normally keep a remote model
identifier in adapter settings and do not use the local model store.
