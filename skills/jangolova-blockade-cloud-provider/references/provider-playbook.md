# Cloud vision provider playbook

A complete, community-owned adapter in four parts: provider skeleton,
response mapping, registration wiring, and tests. Everything lives on the
Grimlock side of the boundary.

## 1. Provider skeleton

```go
package myvision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"jangolova/internal/blockade"
)

// Provider calls a hosted detection or segmentation API and maps the
// response onto blockade.observation/v1alpha1.
type Provider struct {
	// Endpoint is read from the environment by the host; never hardcode it.
	endpoint string
	client   *http.Client
}

func New() (*Provider, error) {
	endpoint := os.Getenv("MYVISION_ENDPOINT")
	if endpoint == "" {
		return nil, fmt.Errorf("MYVISION_ENDPOINT is required")
	}
	return &Provider{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

func (p *Provider) Protocol() string { return "myvision-v1" }

func (p *Provider) Observe(ctx context.Context, request blockade.ObserveRequest) (blockade.ObserveResponse, error) {
	response := blockade.ObserveResponse{
		APIVersion: blockade.APIVersion,
		RequestID:  request.RequestID,
	}
	payload, err := p.call(ctx, request)
	if err != nil {
		return response, fmt.Errorf("myvision observe: %w", err)
	}
	response.Observations = mapDetections(payload)
	if err := blockade.ValidateObserveResponse(response); err != nil {
		return blockade.ObserveResponse{}, err
	}
	return response, nil
}
```

Rules baked into the skeleton:

- The HTTP client carries the timeout; a context-aware request honors caller
  cancellation.
- Credentials come from the environment inside `p.call` (for example
  `os.Getenv("MYVISION_API_KEY")` set on an `Authorization` header). They are
  never fields on exported structs, never logged, never serialized.
- Errors are wrapped with the protocol name but must not include headers,
  URLs with tokens, or raw response bodies.

## 2. Response mapping

Map provider-native fields onto the contract. Keep coordinates in original
pixel space; scale from normalized values before building regions.

| Provider field        | Contract target                     |
| --------------------- | ----------------------------------- |
| class/category name   | `label`                             |
| score/probability     | `confidence` (clamped to `[0,1]`)   |
| bbox (x1,y1,x2,y2)    | `region.x/y/width/height`           |
| segmentation mask     | `mask` as base64-encoded PNG        |
| model/version string  | `evidence`                          |

```go
func mapDetections(payload []byte) []blockade.Observation {
	var parsed struct {
		Detections []struct {
			Name string  `json:"name"`
			Score float64 `json:"score"`
			Box  [4]float64 `json:"bbox"`
		} `json:"detections"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil
	}
	observations := make([]blockade.Observation, 0, len(parsed.Detections))
	for _, detection := range parsed.Detections {
		x1, y1, x2, y2 := detection.Box[0], detection.Box[1], detection.Box[2], detection.Box[3]
		observations = append(observations, blockade.Observation{
			Kind:       "object",
			Label:      detection.Name,
			Confidence: clamp01(detection.Score),
			Region: blockade.Region{
				X: x1, Y: y1,
				Width:  max(x2-x1, 0),
				Height: max(y2-y1, 0),
			},
			Evidence: parsed.Model,
		})
	}
	return observations
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
```

For VLM-style multimodal providers, send pixels plus the prompt and require
structured output; then parse that output into the same observation shape.
VLMs remain `multimodal` providers even though they receive language.

## 3. Registration

```go
provider, err := myvision.New()
if err != nil {
	return err
}
registry, err := grimlock.NewVisionProviderRegistry(provider)
if err != nil {
	return err
}
serviceOptions = append(serviceOptions, grimlock.WithVisionProviderRegistry(registry))
```

`Service.VisionProtocols()` lists registered protocols for health checks and
discovery surfaces.

## 4. Tests

Mapper unit test with a recorded fixture (no network):

```go
func TestMapDetections(t *testing.T) {
	fixture := []byte(`{"model":"m1","detections":[{"name":"bus","score":0.91,"bbox":[2.8,227.5,800.8,736.2]}]}`)
	observations := mapDetections(fixture)
	if err := blockade.ValidateObservations(observations); err != nil {
		t.Fatalf("invalid mapping: %v", err)
	}
	if len(observations) != 1 || observations[0].Label != "bus" {
		t.Fatalf("observations = %#v", observations)
	}
}
```

Live smoke test, gated exactly like the local fixtures:

```go
func TestProviderLiveSmoke(t *testing.T) {
	if os.Getenv("MYVISION_SMOKE") != "1" {
		t.Skip("set MYVISION_SMOKE=1 with live credentials to run")
	}
	// exercise Provider.Observe against the real endpoint and validate.
}
```

Also cover: empty detections, provider 5xx, malformed JSON, context
cancellation, and credential absence (must error cleanly, not panic).

## Boundary constraints

The repository boundary test enforces architecture rules. Do not create
directories named `connector`, `controllers`, `session`, `sessions`,
`surface`, `surfaces`, `vnc`, `webrtc`, or similar under `adapters`, `cmd`,
or `internal`. Deployment files (Dockerfile/Containerfile/compose) belong in
`deploy/` only if allowlisted; prefer keeping cloud adapters free of
deployment topology.

## Reference implementations to study

- Local worker path: [deploy/blockade/app.py](../../deploy/blockade/app.py)
- Native ONNX engine: [internal/blockade/onnx.go](../../internal/blockade/onnx.go)
- Observation types: [internal/blockade/protocol.go](../../internal/blockade/protocol.go)
