package jangolova

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	contract "cymonkey/lib/jangolova/contract"
)

// MacOSPrimitive is one capability reported by a caller-owned macOS helper.
// It describes already-authorized native operations; it never carries script
// source, credentials, TCC state, or a raw Apple Event payload.
type MacOSPrimitive struct {
	Kind        string          `json:"kind"`
	BundleID    string          `json:"bundleId"`
	Name        string          `json:"name,omitempty"`
	AccessGroup string          `json:"accessGroup,omitempty"`
	Settable    bool            `json:"settable,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type MacOSMapping struct {
	Capabilities []contract.Capability `json:"capabilities"`
	Commands     map[string][]string   `json:"commands"`
}

var macOSBundleIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)

// MapMacOSPrimitives maps a negotiated native-helper description into the
// bounded Cymonkey macOS vocabulary. Generic AppleScript and raw Apple Event
// execution are intentionally unmappable.
func MapMacOSPrimitives(primitives []MacOSPrimitive, allowedBundleIDs, allowedCapabilities []string) (MacOSMapping, error) {
	allowedBundles := stringSet(allowedBundleIDs)
	allowedCaps := stringSet(allowedCapabilities)
	result := MacOSMapping{Commands: make(map[string][]string)}
	seenCapabilities := make(map[string]struct{})
	add := func(capability contract.Capability) {
		if len(allowedCaps) != 0 {
			if _, ok := allowedCaps[capability.Name]; !ok {
				return
			}
		}
		if _, ok := seenCapabilities[capability.Name]; ok {
			return
		}
		seenCapabilities[capability.Name] = struct{}{}
		result.Capabilities = append(result.Capabilities, capability)
	}

	for _, primitive := range primitives {
		if !macOSBundleIDPattern.MatchString(primitive.BundleID) {
			return MacOSMapping{}, fmt.Errorf("invalid macOS primitive bundleId %q", primitive.BundleID)
		}
		if len(allowedBundles) != 0 {
			if _, ok := allowedBundles[primitive.BundleID]; !ok {
				continue
			}
		}
		schema := primitive.InputSchema
		if !jsonObjectSchema(schema) {
			schema = objectSchema("surfaceId")
		}
		switch primitive.Kind {
		case "apple-event-command":
			if !safeCommandName(primitive.Name) {
				return MacOSMapping{}, fmt.Errorf("unsafe Apple Event command %q", primitive.Name)
			}
			result.Commands[primitive.BundleID] = stableStrings(append(result.Commands[primitive.BundleID], primitive.Name))
			add(macOSCapability("app.command.list", contract.DriverMacOSAppleEvents, "read", objectSchema("surfaceId")))
			add(macOSCapability("app.command.describe", contract.DriverMacOSAppleEvents, "read", objectSchema("surfaceId", "command")))
			add(macOSCapability("app.command.invoke", contract.DriverMacOSAppleEvents, "external", schema))
		case "accessibility-query":
			add(macOSCapability("ui.query", contract.DriverMacOSAccessibility, "read", schema))
		case "accessibility-observe":
			add(macOSCapability("ui.observe", contract.DriverMacOSAccessibility, "read", schema))
		case "accessibility-action":
			add(macOSCapability("ui.action.invoke", contract.DriverMacOSAccessibility, "write", schema))
		case "accessibility-attribute":
			if primitive.Settable {
				add(macOSCapability("ui.attribute.set", contract.DriverMacOSAccessibility, "write", schema))
			}
		case "applescript", "raw-apple-event", "system-accessibility-tree":
			return MacOSMapping{}, fmt.Errorf("unsafe macOS primitive kind %q is not supported", primitive.Kind)
		default:
			return MacOSMapping{}, fmt.Errorf("unknown macOS primitive kind %q", primitive.Kind)
		}
	}

	sort.Slice(result.Capabilities, func(left, right int) bool {
		return result.Capabilities[left].Name < result.Capabilities[right].Name
	})
	return result, contract.ValidateCapabilities(result.Capabilities)
}

func macOSCapability(name string, driver contract.Driver, effect string, schema json.RawMessage) contract.Capability {
	return contract.Capability{
		Name: name, Domain: contract.DomainViewer, Runtime: "macos-app", Driver: driver,
		Support: contract.SupportMapped, Lifetime: contract.LifetimeAttachment,
		Persistence: contract.PersistenceSession, Effect: effect, InputSchema: schema,
	}
}

func safeCommandName(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && strings.ToLower(value) != "do script" && !strings.ContainsAny(value, "\r\n")
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func jsonObjectSchema(raw json.RawMessage) bool {
	var value map[string]any
	return json.Unmarshal(raw, &value) == nil && value != nil
}
