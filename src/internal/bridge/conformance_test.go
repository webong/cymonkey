package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateConformance(t *testing.T) {
	t.Parallel()

	caller := fakeCaller{
		MethodHello: mustJSON(t, Hello{
			ProtocolVersion: ProtocolVersion,
			Implementation:  Implementation{Name: "fixture", Version: "1"},
			Features:        []string{"events.cursor"},
		}),
		MethodCapabilities: mustJSON(t, []Capability{{
			Name:        "fixture.describe",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Effect:      EffectRead,
		}}),
		MethodDescribe: json.RawMessage(`{"ready":true}`),
		MethodAct:      json.RawMessage(`{"ready":true}`),
		MethodEvents: mustJSON(t, EventBatch{
			Events: []Event{{
				ID:         "1",
				Type:       "fixture.ready",
				OccurredAt: time.Unix(1, 0).UTC(),
				Data:       json.RawMessage(`{"ready":true}`),
			}},
			Cursor: "1",
		}),
	}
	report, err := ValidateConformance(
		context.Background(),
		caller,
		ConformanceOptions{Action: &ActionProbe{
			Name:  "fixture.describe",
			Input: json.RawMessage(`{}`),
		}},
	)
	if err != nil {
		t.Fatalf("ValidateConformance() error = %v", err)
	}
	if report.ProtocolVersion != ProtocolVersion ||
		report.Implementation != "fixture" ||
		report.ActionProbed != "fixture.describe" ||
		report.EventCursor != "1" {
		t.Fatalf("ValidateConformance() = %#v", report)
	}
}

func TestValidateConformanceRejectsInvalidBridge(t *testing.T) {
	t.Parallel()

	valid := fakeCaller{
		MethodHello: mustJSON(t, Hello{
			ProtocolVersion: ProtocolVersion,
			Implementation:  Implementation{Name: "fixture"},
		}),
		MethodCapabilities: mustJSON(t, []Capability{{
			Name:        "fixture.describe",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Effect:      EffectRead,
		}}),
		MethodDescribe: json.RawMessage(`{}`),
		MethodEvents:   mustJSON(t, EventBatch{Cursor: "0"}),
	}
	tests := []struct {
		name   string
		mutate func(fakeCaller)
		want   string
	}{
		{
			name: "protocol",
			mutate: func(caller fakeCaller) {
				caller[MethodHello] = mustJSON(t, Hello{
					ProtocolVersion: "future",
					Implementation:  Implementation{Name: "fixture"},
				})
			},
			want: "incompatible",
		},
		{
			name: "duplicate capability",
			mutate: func(caller fakeCaller) {
				capability := Capability{
					Name:        "same",
					InputSchema: json.RawMessage(`{}`),
					Effect:      EffectRead,
				}
				caller[MethodCapabilities] = mustJSON(
					t,
					[]Capability{capability, capability},
				)
			},
			want: "duplicated",
		},
		{
			name: "cursor",
			mutate: func(caller fakeCaller) {
				caller[MethodEvents] = mustJSON(t, EventBatch{})
			},
			want: "cursor is required",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			caller := valid.clone()
			test.mutate(caller)
			_, err := ValidateConformance(
				context.Background(),
				caller,
				ConformanceOptions{},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateConformance() error = %v, want %q", err, test.want)
			}
		})
	}
}

type fakeCaller map[string]json.RawMessage

func (f fakeCaller) Call(
	_ context.Context,
	method string,
	_ json.RawMessage,
) (json.RawMessage, error) {
	result, exists := f[method]
	if !exists {
		return nil, errors.New("method unavailable")
	}
	return result, nil
}

func (f fakeCaller) clone() fakeCaller {
	clone := make(fakeCaller, len(f))
	for key, value := range f {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
