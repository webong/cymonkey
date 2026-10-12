# Content configuration examples

This directory contains Cymonkey content configurations. Plugin implementations
live under `pkg/` and are installed before
these files are used.

- [`content.example.yaml`](content.example.yaml) selects the source
  Ultralytics plugin and an installed YOLO/SAM model bundle.
- [`content.webllm.example.yaml`](content.webllm.example.yaml) selects the
  WebLLM plugin and a remote WebLLM model ID.
- [`content.endpoints.example.yaml`](content.endpoints.example.yaml) shows
  OpenAI-, Anthropic-, and Google-compatible inference endpoints. The OpenAI
  plugin includes profiles for vLLM, SGLang, Ollama, llama.cpp, and LM Studio.

The old direct Python HTTP service and local `engines` configuration were
removed. The internal content library owns the Python runtime and worker pool. The
Ultralytics package supplies source and vendored dependencies from
[`pkg/content-ultralytics`](../../../pkg/content-ultralytics/).

Install the matching plugin package, install any local model, then validate
and serve:

```sh
cymy content runtime install --manifest /path/to/python/runtime.json
cymy plugins install --manifest /path/to/plugin/package.json
cymy content model install --manifest /path/to/model/model.json \
  --config scripts/deploy/content/content.example.yaml
cymy content validate \
  --config scripts/deploy/content/content.example.yaml --check-files
cymy content serve \
  --config scripts/deploy/content/content.example.yaml \
  --inference local-yolo-sam --bind 127.0.0.1:8091
```

See the [content guide](../../../docs/content.md) and the individual plugin
READMEs for build, package, model, and runtime requirements.
