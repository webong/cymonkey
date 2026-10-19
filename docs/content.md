# Content observation and interpretation

Cymonkey's content domain accepts text, image, audio, and video. A configured
plugin advertises the modalities and operations it supports. Cymonkey owns the
configuration, CLI, HTTP, model, runtime, and plugin contracts.

## Configuration

The default file is `~/.config/cymonkey/content.yaml`. Override it with
`CYMONKEY_CONTENT_CONFIG` or `--config`.

```yaml
apiVersion: cymonkey.content-config/v1alpha1
adapters:
  - id: local-vision
    plugin: onnx
    model: yolo11n@1.0.0
    workers: 1
    timeout: 2m
    maxPayloadBytes: 8388608
```

`plugin` names an installed `content.provider@v1` package. `id` names one
configured adapter and is selected with `--inference`. Secrets contain only
environment references:

```yaml
secrets:
  apiKey:
    env: CONTENT_API_KEY
```

Manage endpoint backed adapters without editing YAML:

```sh
cymy content endpoint add --id local-ollama --profile ollama \
  --url http://127.0.0.1:11434 --model qwen3-vl:8b
cymy content endpoint list
cymy content endpoint test --id local-ollama --text 'hello'
```

Use `--plugin NAME` when adding a custom endpoint plugin.


## Content service package

`cymy content` commands run in the content engine, which is an ext package that
serves `content.service`. Build and install it once:

```sh
go build -o content ./pkg/console/cmd/content
cymy plugins manifest --type content-service --name content --version 1.0.0 --command ./content
cymy plugins install --manifest ./plugin.json
```

The host forwards each command to the package and prints what it wrote, and it
admits only the `command` operation. A command's stdout is its result and comes
back whole with the call; its log lines (stderr) stream to the host as progress
events while it runs, so a model install or `cymy content serve` shows output as
it happens. Events are rate limited by ext, so a command that logs faster than
the limit loses lines, and the host prints how many were dropped. An interrupt
cancels the call and stops `serve`.

The package starts with a scrubbed environment, so the host passes it only the
variables the engine needs to find its configuration and storage, plus any
variable a command or the content configuration names (a `--token-env` value, or
an adapter secret's `env`). The providers (`content.provider` packages) are
hosted by the engine, not by `cymy`.

## Local models and runtimes

Model manifests use `content.model/v1` and the `plugin` field. Runtime
manifests use `content.runtime/v1`.

```sh
cymy content model install --manifest /models/model.json \
  --config ~/.config/cymonkey/content.yaml --adapter local-vision
cymy content model list
cymy content model remove --plugin onnx --model yolo11n@1.0.0

cymy content runtime manifest --source /runtimes/python-tree \
  --name python --version 3.12.8 --executable bin/python3 \
  --output /runtimes/python-tree/runtime.json
cymy content runtime install --manifest /runtimes/python-tree/runtime.json
```

Set `CYMONKEY_MODEL_DIR` and `CYMONKEY_RUNTIME_DIR` to override the Cymonkey
stores.

## Direct requests

```sh
cymy content validate --check-files
cymy content observe --image screenshot.png --inference local-vision
cymy content interpret --text-file notes.txt --prompt 'Summarize'
cymy content interpret --audio recording.wav --audio-type audio/wav \
  --prompt 'Transcribe'
```

The CLI returns `content.observation/v1` or
`content.item/v1` JSON.

## HTTP service

```sh
export CYMONKEY_CONTENT_TOKEN='replace-with-a-random-secret'
cymy content serve --bind 127.0.0.1:8091 \
  --token-env CYMONKEY_CONTENT_TOKEN
cymy content service test --url http://127.0.0.1:8091 \
  --token-env CYMONKEY_CONTENT_TOKEN
```

Routes:

| Route | Contract |
| --- | --- |
| `GET /healthz` | Service readiness |
| `GET /capabilities` | `cymonkey.content-service/v1alpha1` |
| `POST /v1/observe` | `content.observation/v1` request |
| `POST /v1/interpret` | `content.item/v1` request |

The server response never exposes its internal library schema. The operator
uses this same public service contract.

## Plugins

Install or author plugins through the generic Cymonkey package flow. A content
plugin declares `content.provider@v1` and implements methods under the
`content.provider.*` prefix. See [Cymonkey plugins](cymonkey-plugins.md).
