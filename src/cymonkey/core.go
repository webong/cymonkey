// Package cymonkey defines the portable, host-independent Cymonkey contract.
//
// It intentionally depends only on the Go standard library. A host such as
// Jangolova supplies lifecycle, credentials, and concrete browser or runtime
// integrations around these contracts.
package cymonkey

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const ProtocolVersion = "jangolova.cymonkey/v1alpha2"

type Domain string

const (
	DomainComputer Domain = "computer"
	DomainRender   Domain = "render"
	DomainPlayer   Domain = "player"
)

func ValidDomain(domain Domain) bool {
	return domain == DomainComputer || domain == DomainRender || domain == DomainPlayer
}

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)

func ValidIdentifier(value string) bool {
	return identifierPattern.MatchString(strings.TrimSpace(value))
}

type ModuleKind string

const (
	RuntimeModule ModuleKind = "runtime"
	DriverModule  ModuleKind = "driver"
)

// Caller is the portable five-operation Cymonkey surface.
type Caller interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

// Attachment owns any resource created while a module attaches. It must not
// imply ownership of the target runtime itself.
type Attachment interface {
	Caller
	Close(context.Context) error
}

type Endpoint struct {
	Name      string            `json:"name,omitempty"`
	Transport string            `json:"transport"`
	URL       string            `json:"url"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type Target struct {
	ID        string            `json:"id,omitempty"`
	Kind      string            `json:"kind"`
	Endpoints []Endpoint        `json:"endpoints,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func (target Target) Endpoint(transport string) (Endpoint, bool) {
	for _, endpoint := range target.Endpoints {
		if endpoint.Transport == transport {
			return endpoint, true
		}
	}
	return Endpoint{}, false
}

// Policy is deliberately declarative. Hosts enforce credentials, consent, and
// local process policy before giving a module an attachment opportunity.
type Policy struct {
	AllowedCapabilities []string          `json:"allowedCapabilities,omitempty"`
	AllowedOrigins      []string          `json:"allowedOrigins,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
}

type AttachOptions struct {
	Target Target `json:"target"`
	Policy Policy `json:"policy,omitempty"`
}

type RuntimeBinding struct {
	Domain  Domain `json:"domain"`
	Runtime string `json:"runtime"`
}

type DriverDescriptor struct {
	ID         string   `json:"id"`
	Transports []string `json:"transports,omitempty"`
}

type ModuleDescriptor struct {
	ID              string             `json:"id"`
	Kind            ModuleKind         `json:"kind"`
	ProtocolVersion string             `json:"protocolVersion"`
	Runtimes        []RuntimeBinding   `json:"runtimes,omitempty"`
	Drivers         []DriverDescriptor `json:"drivers,omitempty"`
	Description     string             `json:"description,omitempty"`
}

func ValidateModuleDescriptor(descriptor ModuleDescriptor) error {
	if !ValidIdentifier(descriptor.ID) {
		return fmt.Errorf("invalid module id %q", descriptor.ID)
	}
	if descriptor.Kind != RuntimeModule && descriptor.Kind != DriverModule {
		return fmt.Errorf("invalid module kind %q", descriptor.Kind)
	}
	if descriptor.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("module %q requires protocol %q, want %q", descriptor.ID, descriptor.ProtocolVersion, ProtocolVersion)
	}
	if len(descriptor.Runtimes) == 0 {
		return fmt.Errorf("module %q declares no runtimes", descriptor.ID)
	}
	for _, runtime := range descriptor.Runtimes {
		if !ValidDomain(runtime.Domain) || !ValidIdentifier(runtime.Runtime) {
			return fmt.Errorf("module %q has invalid runtime binding", descriptor.ID)
		}
	}
	for _, driver := range descriptor.Drivers {
		if !ValidIdentifier(driver.ID) {
			return fmt.Errorf("module %q has invalid driver id %q", descriptor.ID, driver.ID)
		}
	}
	return nil
}

// Module contributes an explicitly registered runtime or driver. It receives
// only a caller-owned target descriptor and returns a semantic caller.
type Module interface {
	Descriptor() ModuleDescriptor
	Compatible(Target) bool
	Attach(context.Context, AttachOptions) (Attachment, error)
}

type ModuleFunc struct {
	ModuleDescriptor ModuleDescriptor
	CompatibleTarget func(Target) bool
	AttachTarget     func(context.Context, AttachOptions) (Attachment, error)
}

func (module ModuleFunc) Descriptor() ModuleDescriptor { return module.ModuleDescriptor }

func (module ModuleFunc) Compatible(target Target) bool {
	return module.CompatibleTarget == nil || module.CompatibleTarget(target)
}

func (module ModuleFunc) Attach(ctx context.Context, options AttachOptions) (Attachment, error) {
	if module.AttachTarget == nil {
		return nil, fmt.Errorf("module %q has no attachment function", module.ModuleDescriptor.ID)
	}
	return module.AttachTarget(ctx, options)
}

func sortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
