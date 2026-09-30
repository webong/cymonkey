# Blockade architecture

Blockade is a standalone contract and lifecycle library for model backed
observation and interpretation. It accepts text, images, and audio. A provider
performs inference locally or through a cloud service and returns a normalized
Blockade response.

## System composition

```text
agent or application
  -> Cymonkey coordination, when used
  -> Blockade public request
  -> Blockade validation and provider selection
  -> provider package
       -> Blockade-managed local runtime, operating system runtime, browser runtime,
          or cloud model service
  -> Blockade validated response
  -> agent or application decides what happens next
```

Blockade never captures a screen, reads a microphone, controls a target, or
chooses an action. A caller supplies content. In Cymonkey's coordinated image
flow, Jangolova captures approved pixels, Cymonkey passes those pixels to
Blockade, and the caller decides whether to request a later action.

Neither Blockade nor its providers depend on Cymonkey. Cymonkey discovers
installed packages through a generic package inventory and hands packages
that declare `blockade.provider@v1alpha1` to Blockade's plugin adapter. Its
internal orchestration packages do not know provider names such as `onnx`,
`local-ultralytics`, or `webllm`.

## Provider packages

A provider package has two identities:

1. Its package `name`, such as `onnx`. Blockade configuration selects this
   value with `provider: onnx`.
2. A versioned public contract, `blockade.provider@v1alpha1`. The generic
   installer treats this as opaque metadata. Blockade's adapter recognizes it
   and owns the methods behind it.

Example `plugin.json`:

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

`blockade.provider` is the protocol a package implements. It is not an
implementation name and it does not mean Cymonkey contains ONNX or any other
runtime.

The provider process supports a handshake plus Blockade owned methods:

- `blockade.start`
- `blockade.model.inspect`, for providers that accept local models
- `blockade.observe`, when image observation is supported
- `blockade.interpret`, when content interpretation is supported
- `blockade.capabilities`
- `blockade.health`
- `blockade.close`

The request and response envelope is
`blockade.provider-adapter/v1alpha1`. Blockade validates versions, request IDs,
payload bounds, capabilities, normalized output, timeouts, and error kinds.

For a source package, Blockade ownership means Blockade installs and verifies a
complete runtime distribution, chooses the runtime executable, creates the
configured number of processes, and stops them. The provider cannot supply a
host command or fall back to `python3` on `PATH`; it supplies its entrypoint and
the files needed by its inference implementation.

## Configuration

Blockade has one provider list. There is no separate engine configuration.

```yaml
apiVersion: blockade.config/v1alpha1
providers:
  - id: local-yolo
    provider: onnx
    model: yolo11n@1.0.0
    timeout: 30s
    maxPayloadBytes: 8388608
    settings:
      executionProviders: '[{"name":"coreml"},{"name":"cpu"}]'

  - id: hosted-content
    provider: content-cloud
    settings:
      model: remote-model-id
    secrets:
      apiToken:
        env: BLOCKADE_CONTENT_TOKEN
```

`id` names a configured instance. `provider` names an installed package.
`--inference` selects the instance ID. Provider settings remain opaque strings
to Blockade. Credential shaped settings are rejected; secrets use environment
references and are resolved only when requested.

## Local model lifecycle

Provider installation and model installation are separate:

```text
install provider package
  -> Blockade can speak to a runtime implementation

install model manifest and artifacts
  -> verify source digests
  -> stage files
  -> ask the named provider to inspect the staged model
  -> verify digests again
  -> publish name@version atomically

start inference
  -> verify installed digests
  -> pass installed model to the selected provider
  -> Blockade starts configured workers
  -> provider initializes inference
```

The inspection process is separate from provider startup. This lets an ONNX
provider inspect graph inputs and outputs, or another provider verify its own
bundle, without creating the long lived inference service.

```sh
cymonkey blockade model install --manifest /models/model.json \
  --config blockade.yaml
cymonkey blockade model list
cymonkey blockade model remove --provider onnx --model yolo11n@1.0.0
```

The model manifest's `provider` must exactly match the provider package name.
Artifact extensions alone do not prove compatibility. The provider decides
which roles, formats, architectures, and capabilities it accepts.

## Current provider implementations

| Package | Model source | Capabilities |
| --- | --- | --- |
| [`providers/blockade-onnx`](../providers/blockade-onnx/) | Installed YOLO `.onnx` artifact | Image observation and object detection through ONNX Runtime. |
| [`providers/blockade-ultralytics`](../providers/blockade-ultralytics/) | Installed YOLO and SAM `.pt` artifacts | Image observation, detection, and segmentation through a Python worker. |
| [`providers/blockade-webllm`](../providers/blockade-webllm/) | Remote WebLLM model ID and local browser cache | Image observation and vision language output through WebGPU. |
| [`providers/blockade-endpoints`](../providers/blockade-endpoints/) | OpenAI-, Anthropic-, or Google-compatible HTTP endpoint | Text, image, audio, and multimodal interpretation, constrained by the selected protocol and model. |

The providers are repository modules for development and distribution. The
Cymonkey binary does not import them. ONNX and WebLLM build as executables.
Ultralytics is packaged as source and runs with a Python distribution installed
through `cymonkey blockade runtime install`. All use `cymonkey plugins install`
for the provider package itself.

The endpoint providers implement `blockade.interpret`. Their configured
`modalities` setting is an explicit allow-list, so Blockade never assumes that
an OpenAI-compatible server or a selected model supports every content type.
The OpenAI-compatible package includes profiles for vLLM, SGLang, Ollama,
llama.cpp, and LM Studio; custom compatible endpoints override `baseURL`.

## Public APIs

Image observation uses `blockade.observation/v1alpha1`. Content interpretation
uses `blockade.content/v1alpha1` with text, image, or audio parts and findings
that identify their source part indexes.

The service exposes:

- `GET /healthz`
- `GET /capabilities`
- `POST /v1/observe`
- `POST /v1/interpret`

See the [Blockade module README](../lib/blockade/README.md) for commands and
model manifests, the [plugin protocol](cymonkey-plugins.md) for executable
packaging, and the [WebLLM guide](blockade-webllm.md) for that provider.
