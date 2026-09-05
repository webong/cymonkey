// Command native-bridge-fixture is a small cooperative native engine used by
// the bridge transport integration test.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"cymonkey/internal/bridge"
)

type request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type fixture struct {
	value    string
	events   []bridge.Event
	sequence int
}

func main() {
	endpoint := os.Getenv("JANGOLOVA_BRIDGE_URL")
	token := os.Getenv("JANGOLOVA_BRIDGE_TOKEN")
	if endpoint == "" || token == "" {
		fmt.Fprintln(os.Stderr, "native bridge URL and token are required")
		os.Exit(1)
	}
	if got := os.Getenv("JANGOLOVA_BRIDGE_PROTOCOL"); got != bridge.ProtocolVersion {
		fmt.Fprintf(os.Stderr, "unexpected bridge protocol %q\n", got)
		os.Exit(1)
	}
	headers := http.Header{
		"Authorization": []string{"Bearer " + token},
	}
	connection, _, err := websocket.DefaultDialer.Dial(endpoint, headers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect native bridge: %v\n", err)
		os.Exit(1)
	}
	defer connection.Close()
	fmt.Fprintln(os.Stderr, "native bridge fixture connected")

	state := &fixture{value: "initial"}
	for {
		var message request
		if err := connection.ReadJSON(&message); err != nil {
			if websocket.IsCloseError(
				err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
			) {
				return
			}
			fmt.Fprintf(os.Stderr, "read native bridge request: %v\n", err)
			return
		}
		result, callErr := state.call(message.Method, message.Params)
		reply := response{ID: message.ID, Result: result}
		if callErr != nil {
			reply.Result = nil
			reply.Error = &responseError{
				Code:    "fixture_error",
				Message: callErr.Error(),
			}
		}
		if err := connection.WriteJSON(reply); err != nil {
			fmt.Fprintf(os.Stderr, "write native bridge response: %v\n", err)
			return
		}
	}
}

func (f *fixture) call(
	method string,
	params json.RawMessage,
) (json.RawMessage, error) {
	switch method {
	case bridge.MethodHello:
		return encode(bridge.Hello{
			ProtocolVersion: bridge.ProtocolVersion,
			Implementation: bridge.Implementation{
				Name:    "jangolova-native-bridge-fixture",
				Version: "1",
			},
			Features: []string{"events.cursor"},
		})
	case bridge.MethodCapabilities:
		return encode([]bridge.Capability{
			{
				Name:        "fixture.describe",
				Description: "Return the fixture state.",
				InputSchema: json.RawMessage(
					`{"type":"object","additionalProperties":false}`,
				),
				Effect: bridge.EffectRead,
			},
			{
				Name:        "fixture.set",
				Description: "Set the fixture value.",
				InputSchema: json.RawMessage(
					`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`,
				),
				Effect: bridge.EffectWrite,
			},
		})
	case bridge.MethodDescribe:
		return encode(map[string]any{"value": f.value})
	case bridge.MethodAct:
		var action struct {
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(params, &action); err != nil {
			return nil, err
		}
		switch action.Name {
		case "fixture.describe":
			return encode(map[string]any{"value": f.value})
		case "fixture.set":
			var input struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(action.Input, &input); err != nil {
				return nil, err
			}
			if strings.TrimSpace(input.Value) == "" {
				return nil, errors.New("fixture.set value is required")
			}
			f.value = input.Value
			f.publish("fixture.changed", map[string]any{"value": f.value})
			return encode(map[string]any{"ok": true, "value": f.value})
		default:
			return nil, fmt.Errorf("unsupported fixture action %q", action.Name)
		}
	case bridge.MethodEvents:
		var query bridge.EventQuery
		if err := json.Unmarshal(params, &query); err != nil {
			return nil, err
		}
		return encode(f.readEvents(query))
	default:
		return nil, fmt.Errorf("unsupported bridge method %q", method)
	}
}

func (f *fixture) publish(eventType string, data any) {
	f.sequence++
	raw, _ := json.Marshal(data)
	f.events = append(f.events, bridge.Event{
		ID:         strconv.Itoa(f.sequence),
		Type:       eventType,
		OccurredAt: time.Now().UTC(),
		Data:       raw,
	})
}

func (f *fixture) readEvents(query bridge.EventQuery) bridge.EventBatch {
	after, _ := strconv.Atoi(query.After)
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	types := make(map[string]struct{}, len(query.Types))
	for _, eventType := range query.Types {
		types[eventType] = struct{}{}
	}
	events := make([]bridge.Event, 0)
	cursor := after
	for _, event := range f.events {
		sequence, _ := strconv.Atoi(event.ID)
		if sequence <= after {
			continue
		}
		cursor = sequence
		if len(types) != 0 {
			if _, selected := types[event.Type]; !selected {
				continue
			}
		}
		events = append(events, event)
		if len(events) >= limit {
			break
		}
	}
	return bridge.EventBatch{
		Events: events,
		Cursor: strconv.Itoa(cursor),
	}
}

func encode(value any) (json.RawMessage, error) {
	return json.Marshal(value)
}
