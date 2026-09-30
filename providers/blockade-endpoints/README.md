# Blockade endpoint providers

This module builds three executable provider packages for model servers and
hosted inference APIs:

- `openai-compatible` uses `POST /v1/chat/completions`.
- `anthropic-compatible` uses `POST /v1/messages`.
- `google-compatible` uses Gemini `generateContent`.

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
  ./providers/blockade-endpoints/cmd/openai-compatible
go build -o /path/to/anthropic-compatible/provider \
  ./providers/blockade-endpoints/cmd/anthropic-compatible
go build -o /path/to/google-compatible/provider \
  ./providers/blockade-endpoints/cmd/google-compatible
```

Place a `plugin.json` beside each binary. The package `name` must be
`openai-compatible`, `anthropic-compatible`, or `google-compatible`; use the
host platform and the SHA-256 of that binary:

```json
{
  "apiVersion": "provider.plugin/v1alpha1",
  "name": "openai-compatible",
  "version": "1.0.0",
  "contracts": [
    {"name": "blockade.provider", "version": "v1alpha1"}
  ],
  "platform": "darwin-arm64",
  "command": "provider",
  "sha256": "<sha256 of provider>"
}
```

Install each package with `cymonkey plugins install --manifest plugin.json`.

## Configure

All providers require `settings.model`. `modalities` is an explicit
comma-separated allow-list. It defaults to `text`; advertise `image` or
`audio` only when the selected endpoint and model actually support them.
`maxTokens` defaults to `1024`, and `temperature` is optional.

```yaml
apiVersion: blockade.config/v1alpha1
providers:
  - id: local-ollama
    provider: openai-compatible
    settings:
      profile: ollama
      model: qwen3-vl:8b
      modalities: text,image

  - id: hosted-anthropic
    provider: anthropic-compatible
    settings:
      model: claude-sonnet-4-5
      modalities: text,image
    secrets:
      apiKey:
        env: ANTHROPIC_API_KEY

  - id: hosted-gemini
    provider: google-compatible
    settings:
      model: gemini-2.5-flash
      modalities: text,image,audio
    secrets:
      apiKey:
        env: GEMINI_API_KEY
```

OpenAI-compatible settings also accept `profile`, `baseURL`, and
`responseFormat` (`none` or `json_object`). Anthropic-compatible settings
accept `baseURL` and `anthropicVersion`. Google-compatible settings accept
`baseURL`. An `apiKey` secret is optional for local servers and is sent in the
native authorization header when configured.

Gemini's OpenAI compatibility endpoint can alternatively use the
`openai-compatible` provider with its OpenAI-compatible base URL. The native
`google-compatible` provider is the broader mapping for Gemini inline image
and audio parts.

Each provider asks the model for Blockade findings. A valid findings object is
preserved; plain model text is normalized into one `response` finding. HTTP
authentication, rate-limit, timeout, invalid-request, unavailable, and
invalid-response failures retain their Blockade error kind across the plugin
process boundary.
