package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	capabilityPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)
	augmentationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	bundleIDPattern     = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	runtimePattern      = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`)
	driverPattern       = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`)
)

func ValidateHello(value Hello) error {
	if value.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("Jangolova protocol %q is incompatible; expected %q", value.ProtocolVersion, ProtocolVersion)
	}
	if strings.TrimSpace(value.Implementation.Name) == "" || len(value.Domains) == 0 || len(value.Runtimes) == 0 || len(value.Drivers) == 0 {
		return errors.New("Jangolova hello requires implementation, domains, runtimes, and drivers")
	}
	for _, domain := range value.Domains {
		if !ValidDomain(domain) {
			return fmt.Errorf("unsupported Jangolova domain %q", domain)
		}
	}
	for _, runtime := range value.Runtimes {
		if !ValidRuntime(runtime) {
			return fmt.Errorf("invalid Jangolova runtime %q", runtime)
		}
	}
	for _, driver := range value.Drivers {
		if !ValidDriver(driver) {
			return fmt.Errorf("unsupported Jangolova driver %q", driver)
		}
	}
	return nil
}

func ValidateCapabilities(values []Capability) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := string(value.Domain) + ":" + value.Runtime + ":" + value.Name
		if !capabilityPattern.MatchString(value.Name) {
			return fmt.Errorf("invalid Jangolova capability name %q", value.Name)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate Jangolova capability %q for domain %q and runtime %q", value.Name, value.Domain, value.Runtime)
		}
		seen[key] = struct{}{}
		if !ValidDomain(value.Domain) || !ValidRuntime(value.Runtime) || !driverSupportsDomain(value.Driver, value.Domain) {
			return fmt.Errorf("Jangolova capability %q has incompatible domain/runtime/driver", value.Name)
		}
		if value.Support != SupportNative && value.Support != SupportMapped && value.Support != SupportEmulated {
			return fmt.Errorf("Jangolova capability %q has invalid support", value.Name)
		}
		if value.Lifetime != LifetimeCall && value.Lifetime != LifetimeSurface && value.Lifetime != LifetimeAttachment && value.Lifetime != LifetimeInstallation {
			return fmt.Errorf("Jangolova capability %q has invalid lifetime", value.Name)
		}
		if value.Persistence != PersistenceEphemeral && value.Persistence != PersistenceSession && value.Persistence != PersistencePersistent {
			return fmt.Errorf("Jangolova capability %q has invalid persistence", value.Name)
		}
		if value.Effect != "read" && value.Effect != "write" && value.Effect != "external" {
			return fmt.Errorf("Jangolova capability %q has invalid effect", value.Name)
		}
		var schema map[string]any
		if json.Unmarshal(value.InputSchema, &schema) != nil || schema == nil {
			return fmt.Errorf("Jangolova capability %q requires an input schema object", value.Name)
		}
	}
	return nil
}

func ValidateManifest(value Manifest) error {
	if value.APIVersion != ProtocolVersion || value.Kind != AugmentationKind {
		return fmt.Errorf("augmentation requires %s and kind Augmentation", ProtocolVersion)
	}
	if !augmentationPattern.MatchString(value.Metadata.ID) || strings.TrimSpace(value.Metadata.Revision) == "" {
		return errors.New("Jangolova augmentation requires a stable id and revision")
	}
	if len(value.Spec.Targets) == 0 {
		return errors.New("Jangolova augmentation requires at least one target")
	}
	domains := make(map[Domain]struct{})
	for _, target := range value.Spec.Targets {
		if !ValidDomain(target.Domain) {
			return fmt.Errorf("unsupported Jangolova target domain %q", target.Domain)
		}
		if !ValidRuntime(target.Runtime) {
			return fmt.Errorf("invalid Jangolova target runtime %q", target.Runtime)
		}
		domains[target.Domain] = struct{}{}
		if err := validateTarget(target); err != nil {
			return err
		}
	}
	for _, permission := range value.Spec.Permissions {
		if !capabilityPattern.MatchString(permission) {
			return fmt.Errorf("invalid Jangolova permission %q", permission)
		}
	}
	if len(bytes.TrimSpace(value.Spec.Viewer)) > 0 {
		if _, ok := domains[DomainViewer]; !ok {
			return errors.New("Jangolova viewer payload requires a viewer target")
		}
		if !jsonObject(value.Spec.Viewer) {
			return errors.New("Jangolova viewer payload must be an object")
		}
	}
	if len(bytes.TrimSpace(value.Spec.Render)) > 0 {
		if _, ok := domains[DomainRender]; !ok {
			return errors.New("Jangolova render payload requires a render target")
		}
		if !jsonObject(value.Spec.Render) {
			return errors.New("Jangolova render payload must be an object")
		}
	}
	if len(bytes.TrimSpace(value.Spec.Player)) > 0 {
		if _, ok := domains[DomainPlayer]; !ok {
			return errors.New("Jangolova player payload requires a player target")
		}
		if !jsonObject(value.Spec.Player) {
			return errors.New("Jangolova player payload must be an object")
		}
	}
	return nil
}

func ValidDomain(value Domain) bool {
	return value == DomainViewer || value == DomainRender || value == DomainPlayer
}

func ValidRuntime(value string) bool {
	return runtimePattern.MatchString(value)
}

func ValidDriver(value Driver) bool {
	return driverPattern.MatchString(string(value))
}

func driverSupportsDomain(driver Driver, domain Domain) bool {
	if !ValidDriver(driver) {
		return false
	}
	switch driver {
	case DriverCDP, DriverBiDi, DriverSafariMCP, DriverWebExtension,
		DriverMacOSAppleEvents, DriverMacOSAccessibility, DriverMacOSCooperative,
		DriverWebSocket, DriverInPageRuntime:
		return knownDriverSupportsDomain(driver, domain)
	default:
		// A contributor-declared driver is valid for its advertised module
		// binding. The module registry and capability negotiation, rather than a
		// closed core enum, provide the semantic authority.
		return true
	}
}

func knownDriverSupportsDomain(driver Driver, domain Domain) bool {
	if domain == DomainViewer {
		return driver == DriverCDP || driver == DriverBiDi || driver == DriverSafariMCP || driver == DriverWebExtension ||
			driver == DriverMacOSAppleEvents || driver == DriverMacOSAccessibility || driver == DriverMacOSCooperative
	}
	if domain == DomainRender {
		return driver == DriverCDP || driver == DriverBiDi || driver == DriverSafariMCP || driver == DriverWebExtension || driver == DriverWebSocket || driver == DriverInPageRuntime
	}
	if domain == DomainPlayer {
		return true
	}
	return false
}

func validateTarget(target Target) error {
	var match map[string]any
	if json.Unmarshal(target.Match, &match) != nil || match == nil {
		return fmt.Errorf("Jangolova %s target match must be an object", target.Domain)
	}
	switch {
	case (target.Domain == DomainViewer || target.Domain == DomainRender) && target.Runtime == "browser-dom":
		patterns, ok := match["urlPatterns"].([]any)
		if !ok || len(patterns) == 0 {
			return fmt.Errorf("Jangolova browser-dom %s target requires urlPatterns", target.Domain)
		}
	case target.Domain == DomainViewer && target.Runtime == "macos-app":
		bundleID, _ := match["bundleId"].(string)
		if !bundleIDPattern.MatchString(bundleID) {
			return errors.New("Jangolova macos-app viewer target requires a valid bundleId")
		}
	}
	return nil
}

func jsonObject(raw json.RawMessage) bool {
	var value map[string]any
	return json.Unmarshal(raw, &value) == nil && value != nil
}
