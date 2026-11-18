package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Caller invokes one bridge operation and returns its JSON result. Native and
// browser transports can both implement this boundary.
type Caller interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

// ActionProbe identifies one explicitly safe action for a conformance run.
type ActionProbe struct {
	Name  string
	Input json.RawMessage
}

type ConformanceOptions struct {
	Action *ActionProbe
}

type ConformanceReport struct {
	ProtocolVersion string   `json:"protocolVersion"`
	Implementation  string   `json:"implementation"`
	Capabilities    []string `json:"capabilities"`
	ActionProbed    string   `json:"actionProbed,omitempty"`
	EventCursor     string   `json:"eventCursor"`
}

// ValidateConformance checks handshake, capability, observation, optional
// action, and event behavior without assuming an engine-specific scene model.
func ValidateConformance(
	ctx context.Context,
	caller Caller,
	options ConformanceOptions,
) (ConformanceReport, error) {
	if caller == nil {
		return ConformanceReport{}, errors.New("bridge conformance caller is required")
	}

	helloRaw, err := caller.Call(ctx, MethodHello, json.RawMessage(`{}`))
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("bridge hello: %w", err)
	}
	var hello Hello
	if err := decodeResult(MethodHello, helloRaw, &hello); err != nil {
		return ConformanceReport{}, err
	}
	if hello.ProtocolVersion != ProtocolVersion {
		return ConformanceReport{}, fmt.Errorf(
			"bridge protocol %q is incompatible; expected %q",
			hello.ProtocolVersion,
			ProtocolVersion,
		)
	}
	if strings.TrimSpace(hello.Implementation.Name) == "" {
		return ConformanceReport{}, errors.New("bridge implementation name is required")
	}
	if err := validateUniqueStrings("bridge feature", hello.Features); err != nil {
		return ConformanceReport{}, err
	}

	capabilitiesRaw, err := caller.Call(
		ctx,
		MethodCapabilities,
		json.RawMessage(`{}`),
	)
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("bridge capabilities: %w", err)
	}
	var capabilities []Capability
	if err := decodeResult(MethodCapabilities, capabilitiesRaw, &capabilities); err != nil {
		return ConformanceReport{}, err
	}
	capabilityNames, err := validateCapabilities(capabilities)
	if err != nil {
		return ConformanceReport{}, err
	}

	description, err := caller.Call(ctx, MethodDescribe, json.RawMessage(`{}`))
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("bridge describe: %w", err)
	}
	if !validJSONValue(description) {
		return ConformanceReport{}, errors.New("bridge describe returned invalid JSON")
	}

	report := ConformanceReport{
		ProtocolVersion: hello.ProtocolVersion,
		Implementation:  hello.Implementation.Name,
		Capabilities:    capabilityNames,
	}
	if options.Action != nil {
		if err := validateActionProbe(*options.Action, capabilities); err != nil {
			return ConformanceReport{}, err
		}
		input := options.Action.Input
		if len(bytes.TrimSpace(input)) == 0 {
			input = json.RawMessage(`{}`)
		}
		request, _ := json.Marshal(map[string]any{
			"name":  options.Action.Name,
			"input": json.RawMessage(input),
		})
		result, err := caller.Call(ctx, MethodAct, request)
		if err != nil {
			return ConformanceReport{}, fmt.Errorf(
				"bridge action probe %q: %w",
				options.Action.Name,
				err,
			)
		}
		if !validJSONValue(result) {
			return ConformanceReport{}, fmt.Errorf(
				"bridge action probe %q returned invalid JSON",
				options.Action.Name,
			)
		}
		report.ActionProbed = options.Action.Name
	}

	query, _ := json.Marshal(EventQuery{Limit: 10})
	eventsRaw, err := caller.Call(ctx, MethodEvents, query)
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("bridge events: %w", err)
	}
	var batch EventBatch
	if err := decodeResult(MethodEvents, eventsRaw, &batch); err != nil {
		return ConformanceReport{}, err
	}
	if err := validateEventBatch(batch); err != nil {
		return ConformanceReport{}, err
	}
	report.EventCursor = batch.Cursor
	return report, nil
}

func decodeResult(method string, raw json.RawMessage, target any) error {
	if !validJSONValue(raw) {
		return fmt.Errorf("bridge %s returned invalid JSON", method)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode bridge %s result: %w", method, err)
	}
	return nil
}

func validJSONValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null")) && json.Valid(trimmed)
}

func validateCapabilities(
	capabilities []Capability,
) ([]string, error) {
	names := make([]string, 0, len(capabilities))
	seen := make(map[string]struct{}, len(capabilities))
	for index, capability := range capabilities {
		name := strings.TrimSpace(capability.Name)
		if name == "" {
			return nil, fmt.Errorf("bridge capability %d name is required", index)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("bridge capability %q is duplicated", name)
		}
		seen[name] = struct{}{}
		switch capability.Effect {
		case EffectRead,
			EffectWrite,
			EffectExternal:
		default:
			return nil, fmt.Errorf(
				"bridge capability %q has invalid effect %q",
				name,
				capability.Effect,
			)
		}
		var schema map[string]any
		if err := json.Unmarshal(capability.InputSchema, &schema); err != nil || schema == nil {
			return nil, fmt.Errorf(
				"bridge capability %q inputSchema must be a JSON object",
				name,
			)
		}
		names = append(names, name)
	}
	return names, nil
}

func validateActionProbe(
	probe ActionProbe,
	capabilities []Capability,
) error {
	if strings.TrimSpace(probe.Name) == "" {
		return errors.New("bridge action probe name is required")
	}
	if len(bytes.TrimSpace(probe.Input)) != 0 && !json.Valid(probe.Input) {
		return fmt.Errorf("bridge action probe %q input is invalid JSON", probe.Name)
	}
	for _, capability := range capabilities {
		if capability.Name != probe.Name {
			continue
		}
		if capability.Effect != EffectRead {
			return fmt.Errorf(
				"bridge action probe %q must identify a read capability, got %q",
				probe.Name,
				capability.Effect,
			)
		}
		return nil
	}
	return fmt.Errorf(
		"bridge action probe %q is not an advertised capability",
		probe.Name,
	)
}

func validateEventBatch(batch EventBatch) error {
	if strings.TrimSpace(batch.Cursor) == "" {
		return errors.New("bridge events cursor is required")
	}
	seen := make(map[string]struct{}, len(batch.Events))
	for index, event := range batch.Events {
		if strings.TrimSpace(event.ID) == "" {
			return fmt.Errorf("bridge event %d id is required", index)
		}
		if _, exists := seen[event.ID]; exists {
			return fmt.Errorf("bridge event id %q is duplicated", event.ID)
		}
		seen[event.ID] = struct{}{}
		if strings.TrimSpace(event.Type) == "" {
			return fmt.Errorf("bridge event %q type is required", event.ID)
		}
		if event.OccurredAt.IsZero() {
			return fmt.Errorf("bridge event %q occurredAt is required", event.ID)
		}
		if len(event.Data) != 0 && !json.Valid(event.Data) {
			return fmt.Errorf("bridge event %q data is invalid JSON", event.ID)
		}
	}
	return nil
}

func validateUniqueStrings(label string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("%s cannot be empty", label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s %q is duplicated", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}
