package cymonkey

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
)

func ValidateHello(value Hello) error {
	if value.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("Cymonkey protocol %q is incompatible; expected %q", value.ProtocolVersion, ProtocolVersion)
	}
	if strings.TrimSpace(value.Implementation.Name) == "" || len(value.Profiles) == 0 || len(value.Backends) == 0 {
		return errors.New("Cymonkey hello requires implementation, profiles, and backends")
	}
	for _, profile := range value.Profiles {
		if !ValidProfile(profile) {
			return fmt.Errorf("unsupported Cymonkey profile %q", profile)
		}
	}
	for _, backend := range value.Backends {
		if !ValidBackend(backend) {
			return fmt.Errorf("unsupported Cymonkey backend %q", backend)
		}
	}
	return nil
}

func ValidateCapabilities(values []Capability) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := string(value.Profile) + ":" + value.Name
		if !capabilityPattern.MatchString(value.Name) {
			return fmt.Errorf("invalid Cymonkey capability name %q", value.Name)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate Cymonkey capability %q for profile %q", value.Name, value.Profile)
		}
		seen[key] = struct{}{}
		if !ValidProfile(value.Profile) || !backendSupportsProfile(value.Backend, value.Profile) {
			return fmt.Errorf("Cymonkey capability %q has incompatible profile/backend", value.Name)
		}
		if value.Support != SupportNative && value.Support != SupportMapped && value.Support != SupportEmulated {
			return fmt.Errorf("Cymonkey capability %q has invalid support", value.Name)
		}
		if value.Lifetime != LifetimeCall && value.Lifetime != LifetimeSurface && value.Lifetime != LifetimeAttachment && value.Lifetime != LifetimeInstallation {
			return fmt.Errorf("Cymonkey capability %q has invalid lifetime", value.Name)
		}
		if value.Persistence != PersistenceEphemeral && value.Persistence != PersistenceSession && value.Persistence != PersistencePersistent {
			return fmt.Errorf("Cymonkey capability %q has invalid persistence", value.Name)
		}
		if value.Effect != "read" && value.Effect != "write" && value.Effect != "external" {
			return fmt.Errorf("Cymonkey capability %q has invalid effect", value.Name)
		}
		var schema map[string]any
		if json.Unmarshal(value.InputSchema, &schema) != nil || schema == nil {
			return fmt.Errorf("Cymonkey capability %q requires an input schema object", value.Name)
		}
	}
	return nil
}

func ValidateManifest(value Manifest) error {
	if value.APIVersion != ProtocolVersion || value.Kind != AugmentationKind {
		return errors.New("Cymonkey augmentation requires v1alpha2 and kind Augmentation")
	}
	if !augmentationPattern.MatchString(value.Metadata.ID) || strings.TrimSpace(value.Metadata.Revision) == "" {
		return errors.New("Cymonkey augmentation requires a stable id and revision")
	}
	if len(value.Spec.Targets) == 0 {
		return errors.New("Cymonkey augmentation requires at least one target")
	}
	profiles := make(map[Profile]struct{})
	for _, target := range value.Spec.Targets {
		if !ValidProfile(target.Profile) {
			return fmt.Errorf("unsupported Cymonkey target profile %q", target.Profile)
		}
		profiles[target.Profile] = struct{}{}
		if err := validateTarget(target); err != nil {
			return err
		}
	}
	for _, permission := range value.Spec.Permissions {
		if !capabilityPattern.MatchString(permission) {
			return fmt.Errorf("invalid Cymonkey permission %q", permission)
		}
	}
	if len(bytes.TrimSpace(value.Spec.Web)) > 0 {
		if _, ok := profiles[ProfileWeb]; !ok {
			return errors.New("Cymonkey web payload requires a web target")
		}
		if !jsonObject(value.Spec.Web) {
			return errors.New("Cymonkey web payload must be an object")
		}
	}
	if len(bytes.TrimSpace(value.Spec.MacOS)) > 0 {
		if _, ok := profiles[ProfileMacOS]; !ok {
			return errors.New("Cymonkey macOS payload requires a macOS target")
		}
		if !jsonObject(value.Spec.MacOS) {
			return errors.New("Cymonkey macOS payload must be an object")
		}
	}
	return nil
}

func ValidProfile(value Profile) bool {
	return value == ProfileWeb || value == ProfileMacOS || value == ProfileEngine
}

func ValidBackend(value Backend) bool {
	switch value {
	case BackendCDP, BackendBiDi, BackendSafariMCP, BackendWebExtension,
		BackendMacOSAppleEvents, BackendMacOSAccessibility, BackendMacOSCooperative,
		BackendEngineGodot, BackendEngineUnity, BackendEngineUnreal, BackendEngineThreejs, BackendEnginePacman:
		return true
	default:
		return false
	}
}

func backendSupportsProfile(backend Backend, profile Profile) bool {
	if !ValidBackend(backend) {
		return false
	}
	if profile == ProfileWeb {
		return backend == BackendCDP || backend == BackendBiDi || backend == BackendSafariMCP || backend == BackendWebExtension
	}
	if profile == ProfileMacOS {
		return backend == BackendMacOSAppleEvents || backend == BackendMacOSAccessibility || backend == BackendMacOSCooperative
	}
	if profile == ProfileEngine {
		return backend == BackendEngineGodot || backend == BackendEngineUnity || backend == BackendEngineUnreal || backend == BackendEngineThreejs || backend == BackendEnginePacman
	}
	return false
}

func validateTarget(target Target) error {
	var match map[string]any
	if json.Unmarshal(target.Match, &match) != nil || match == nil {
		return fmt.Errorf("Cymonkey %s target match must be an object", target.Profile)
	}
	switch target.Profile {
	case ProfileWeb:
		patterns, ok := match["urlPatterns"].([]any)
		if !ok || len(patterns) == 0 {
			return errors.New("Cymonkey web target requires urlPatterns")
		}
	case ProfileMacOS:
		bundleID, _ := match["bundleId"].(string)
		if !bundleIDPattern.MatchString(bundleID) {
			return errors.New("Cymonkey macOS target requires a valid bundleId")
		}
	}
	return nil
}

func jsonObject(raw json.RawMessage) bool {
	var value map[string]any
	return json.Unmarshal(raw, &value) == nil && value != nil
}
