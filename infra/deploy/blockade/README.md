# Blockade configuration examples

This directory contains provider only Blockade configurations. Provider
implementations live under `providers/` and are installed as packages before
these files are used.

- [`blockade.example.yaml`](blockade.example.yaml) selects the standalone
  Ultralytics provider and an installed YOLO/SAM model bundle.
- [`blockade.webllm.example.yaml`](blockade.webllm.example.yaml) selects the
  standalone WebLLM provider and a remote WebLLM model ID.
- [`blockade.endpoints.example.yaml`](blockade.endpoints.example.yaml) shows
  OpenAI-, Anthropic-, and Google-compatible inference endpoints. The OpenAI
  provider includes profiles for vLLM, SGLang, Ollama, llama.cpp, and LM Studio.

The old direct Python HTTP service and local `engines` configuration were
removed. Blockade now owns the installed Python runtime and worker pool. The
Ultralytics package supplies source and vendored dependencies from
[`providers/blockade-ultralytics`](../../../providers/blockade-ultralytics/).

Install the matching provider package, install any local model, then validate
and serve:

```sh
cymonkey blockade runtime install --manifest /path/to/python/runtime.json
cymonkey plugins install --manifest /path/to/provider/plugin.json
cymonkey blockade model install --manifest /path/to/model/model.json \
  --config infra/deploy/blockade/blockade.example.yaml
cymonkey blockade validate \
  --config infra/deploy/blockade/blockade.example.yaml --check-files
cymonkey blockade serve \
  --config infra/deploy/blockade/blockade.example.yaml \
  --inference local-yolo-sam --bind 127.0.0.1:8091
```

See the [Blockade architecture](../../../docs/blockade.md) and the individual
provider READMEs for build, package, model, and runtime requirements.
