package cymonkeyprotocol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestCymonkeyV1alpha2GeneratedBindingsRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	original := Hello{
		ProtocolVersion: ProtocolVersion,
		Implementation:  Implementation{Name: "fixture", Version: "1.0"},
		Domains:         []DomainName{DomainViewer, DomainRender},
		Runtimes:        []string{"browser-dom", "threejs"},
		Drivers:         []DriverName{DriverCDP, DriverInPageRuntime},
		Features:        []string{"script", "dom"},
	}
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	var decoded Hello
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal hello: %v", err)
	}
	if diff := cmp.Diff(original, decoded); diff != "" {
		t.Fatalf("hello round-trip mismatch: %s", diff)
	}

	eventBatch := EventBatch{
		Cursor: "1",
		Events: []Event{
			{
				ID:         "evt-1",
				Type:       "cymonkey.augmentation.applied",
				OccurredAt: now,
				Domain:     DomainViewer,
				Runtime:    "browser-dom",
				Driver:     DriverCDP,
				SurfaceID:  "web:tab-1",
				Data:       json.RawMessage(`{"augmentationId":"script-1"}`),
			},
		},
	}
	payload, err = json.Marshal(eventBatch)
	if err != nil {
		t.Fatalf("marshal eventBatch: %v", err)
	}
	var decodedBatch EventBatch
	if err := json.Unmarshal(payload, &decodedBatch); err != nil {
		t.Fatalf("unmarshal eventBatch: %v", err)
	}
	if diff := cmp.Diff(eventBatch, decodedBatch); diff != "" {
		t.Fatalf("eventBatch round-trip mismatch: %s", diff)
	}
}

func TestCymonkeyV1alpha2ClientCall(t *testing.T) {
	t.Parallel()
	var captured CallRequest
	transport := &fakeTransport{
		response: CallResponse{ProtocolVersion: ProtocolVersion, Result: json.RawMessage(`{"ok":true}`)},
		onCall: func(req CallRequest) {
			captured = req
		},
	}
	client := Client{Transport: transport}
	result, err := client.Call(context.Background(), "cymonkey.augmentation.apply", json.RawMessage(`{"id":"aug-1"}`))
	if err != nil {
		t.Fatalf("client call: %v", err)
	}
	if string(result) != `{"ok":true}` {
		t.Fatalf("result = %s, want {\"ok\":true}", result)
	}
	if captured.Method != "cymonkey.augmentation.apply" {
		t.Fatalf("method = %q, want cymonkey.augmentation.apply", captured.Method)
	}
}

type fakeTransport struct {
	response CallResponse
	onCall   func(CallRequest)
}

func (f *fakeTransport) Call(_ context.Context, req CallRequest) (CallResponse, error) {
	if f.onCall != nil {
		f.onCall(req)
	}
	return f.response, nil
}
