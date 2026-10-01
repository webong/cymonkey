# Cymonkey endpoint content plugins

This module builds three executable content plugins for model servers and
hosted inference APIs:

- `openai-compatible` uses `POST /v1/chat/completions`.
- `anthropic-compatible` uses `POST /v1/messages`.
- `google-compatible` uses Gemini `generateContent`.

For native embedding, the optional [`generation`](generation/README.md) and
[`realtime`](realtime/README.md) packages implement Blockade's generation and
speech contracts. Those SDK packages require explicit endpoint/model settings
and have no built-in provider profiles. The CLI profiles below belong to the
existing observation/interpretation executables.

The OpenAI-compatible provider has profiles for the common local servers. A
profile supplies only the conventional local base URL; `baseURL` can always
override it.

| Profile | Default base URL |
| --- | --- |
| `vllm` | `http://127.0.0.1:8000/v1` |
| `sglang` | `http://127.0.0.1:30000/v1` |
| `ollama` | `http://127.0.0.1:11434/v1` |
| `llama.cpp` | `http://127.0.0.1:8080/v1` |
| `lm-studio` | `http://127.0.0.1:1234/v1` |
| `openai` | `https://api.openai.com/v1` |
| `custom` | `http://127.0.0.1:8000/v1` |

## Build and package

Build the executable that matches the package name:

```sh
go build -o /path/to/openai-compatible/provider \
  ./providers/content-endpoints/cmd/openai-compatible
go build -o /path/to/anthropic-compatible/provider \
  ./providers/content-endpoints/cmd/anthropic-compatible
go build -o /path/to/google-compatible/provider \
  ./providers/content-endpoints/cmd/google-compatible
```

Place a `plugin.json` beside each binary. The package `name` must be
`openai-compatible`, `anthropic-compatible`, or `google-compatible`; use the
host platform and the SHA-256 of that binary:

```json
{
  "apiVersion": "cymonkey.plugin/v1alpha1",
  "name": "openai-compatible",
  "version": "1.0.0",
  "contracts": [
    {"name": "cymonkey.content", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "command": "provider",
  "sha256": "<sha256 of provider>"
}
```

Generate the reviewed manifest and install each plugin through Cymonkey:

```sh
cymy plugins manifest --type content --name openai-compatible --version 1.0.0 \
  --command /path/to/openai-compatible/provider
cymy plugins install \
  --manifest /path/to/openai-compatible/plugin.json
```

## Configure

All providers require `settings.model`. `modalities` is an explicit
comma-separated allow-list. It defaults to `text`; advertise `image`, `audio`,
or `video` only when the selected endpoint and model actually support them.
`maxTokens` defaults to `1024`, and `temperature` is optional.

```yaml
apiVersion: cymonkey.content-config/v1alpha1
adapters:
  - id: local-ollama
    plugin: openai-compatible
    settings:
      profile: ollama
      model: qwen3-vl:8b
      modalities: text,image

  - id: hosted-anthropic
    plugin: anthropic-compatible
    settings:
      model: claude-sonnet-4-5
      modalities: text,image
    secrets:
      apiKey:
        env: ANTHROPIC_API_KEY

  - id: hosted-gemini
    plugin: google-compatible
    settings:
      model: gemini-2.5-flash
      modalities: text,image,audio,video
    secrets:
      apiKey:
        env: GEMINI_API_KEY
```

OpenAI-compatible settings also accept `profile`, `baseURL`, and
`responseFormat` (`none` or `json_object`). Video uses a `video_url` content
part by default; the `llama.cpp` profile uses `input_video`. Set
`videoContentType` to `video_url` or `input_video` to override that choice for
a compatible server. Anthropic-compatible settings
accept `baseURL` and `anthropicVersion`. Google-compatible settings accept
`baseURL`. An `apiKey` secret is optional for local servers and is sent in the
native authorization header when configured.

The CLI writes the equivalent configuration and keeps API keys in the
environment:

```sh
cymy content endpoint add --id local-ollama --profile ollama \
  --model qwen3-vl:8b --modalities text,image
cymy content endpoint add --id hosted-gemini --profile gemini \
  --model gemini-2.5-flash --modalities text,image,audio,video \
  --api-key-env GEMINI_API_KEY
cymy content endpoint test --id local-ollama
```

Gemini's OpenAI compatibility endpoint can alternatively use the
`openai-compatible` provider with its OpenAI-compatible base URL. The native
`google-compatible` provider is the broader mapping for Gemini inline image,
audio, and video parts. Anthropic Messages has no native video content block,
so the Anthropic-compatible adapter intentionally remains text-and-image only.

The current Cymonkey content request limit is 10 MiB, so video is sent inline.
Use short clips that fit that bound; hosted upload/file APIs are outside this
adapter contract.

Each plugin asks the model for Cymonkey findings. A valid findings object is
preserved; plain model text is normalized into one `response` finding. HTTP
authentication, rate-limit, timeout, invalid-request, unavailable, and
invalid-response failures retain their stable error kind across the plugin
process boundary.
