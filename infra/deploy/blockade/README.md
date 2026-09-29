# Blockade configuration examples

This directory contains provider only Blockade configurations. Provider
runtime implementations live under `providers/` and are installed as
executable packages before these files are used.

- [`blockade.example.yaml`](blockade.example.yaml) selects the standalone
  Ultralytics provider and an installed YOLO/SAM model bundle.
- [`blockade.webllm.example.yaml`](blockade.webllm.example.yaml) selects the
  standalone WebLLM provider and a remote WebLLM model ID.

The old direct Python HTTP service and local `engines` configuration were
removed. The Python worker is now embedded in the Ultralytics provider
executable; its source and pinned dependencies are in
[`providers/blockade-ultralytics`](../../../providers/blockade-ultralytics/).

Install the matching provider package, install any local model, then validate
and serve:

```sh
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
