package cymonkey

import (
	"context"
	"encoding/json"
	"fmt"
)

type Hello struct {
	ProtocolVersion string   `json:"protocolVersion"`
	Domains         []Domain `json:"domains"`
	Runtimes        []string `json:"runtimes"`
	Drivers         []string `json:"drivers"`
}

type Capability struct {
	Name        string          `json:"name"`
	Domain      Domain          `json:"domain"`
	Runtime     string          `json:"runtime"`
	Driver      string          `json:"driver,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ConformanceReport struct {
	Hello        Hello        `json:"hello"`
	Capabilities []Capability `json:"capabilities"`
}

// ValidateConformance verifies the public five-operation surface without
// assigning any meaning to host-specific describe or event payloads.
func ValidateConformance(ctx context.Context, caller Caller) (ConformanceReport, error) {
	if caller == nil {
		return ConformanceReport{}, fmt.Errorf("Cymonkey caller is nil")
	}
	var report ConformanceReport
	hello, err := caller.Call(ctx, "hello", json.RawMessage(`{}`))
	if err != nil {
		return report, fmt.Errorf("call hello: %w", err)
	}
	if err := json.Unmarshal(hello, &report.Hello); err != nil {
		return report, fmt.Errorf("decode hello: %w", err)
	}
	if report.Hello.ProtocolVersion != ProtocolVersion {
		return report, fmt.Errorf("protocol version %q, want %q", report.Hello.ProtocolVersion, ProtocolVersion)
	}
	capabilities, err := caller.Call(ctx, "capabilities", json.RawMessage(`{}`))
	if err != nil {
		return report, fmt.Errorf("call capabilities: %w", err)
	}
	if err := json.Unmarshal(capabilities, &report.Capabilities); err != nil {
		return report, fmt.Errorf("decode capabilities: %w", err)
	}
	for _, capability := range report.Capabilities {
		if capability.Name == "" || !ValidDomain(capability.Domain) || !ValidIdentifier(capability.Runtime) {
			return report, fmt.Errorf("invalid capability descriptor %q", capability.Name)
		}
		if len(capability.InputSchema) == 0 || !json.Valid(capability.InputSchema) {
			return report, fmt.Errorf("capability %q has invalid input schema", capability.Name)
		}
	}
	for _, method := range []string{"describe", "events"} {
		if _, err := caller.Call(ctx, method, json.RawMessage(`{}`)); err != nil {
			return report, fmt.Errorf("call %s: %w", method, err)
		}
	}
	return report, nil
}

func ValidateModuleConformance(ctx context.Context, module Module, caller Caller) (ConformanceReport, error) {
	if module == nil {
		return ConformanceReport{}, fmt.Errorf("Cymonkey module is nil")
	}
	if err := ValidateModuleDescriptor(module.Descriptor()); err != nil {
		return ConformanceReport{}, err
	}
	return ValidateConformance(ctx, caller)
}
