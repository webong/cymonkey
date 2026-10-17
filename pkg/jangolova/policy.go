package jangolova

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	contract "jangolova/contract"
)

func normalizeOptions(value *Options) error {
	value.Domain = contract.Domain(strings.ToLower(strings.TrimSpace(string(value.Domain))))
	if value.Domain != "" && !contract.ValidDomain(value.Domain) {
		return fmt.Errorf("unsupported Jangolova domain %q", value.Domain)
	}
	value.Module = strings.TrimSpace(value.Module)
	value.Driver = strings.ToLower(strings.TrimSpace(value.Driver))
	if value.Driver == "" {
		value.Driver = "auto"
	}
	if value.Composite != nil && value.Driver != "auto" {
		return errors.New("Jangolova composite selects its drivers per binding; do not set top-level driver")
	}
	switch value.Driver {
	case "auto", "playwright", "puppeteer", string(BackendCDP), string(BackendBiDi), string(BackendSafariMCP),
		string(BackendMacOSAppleEvents), string(BackendMacOSAccessibility), string(BackendMacOSCooperative), string(BackendWindowsCooperative):
	default:
		if !contract.ValidDriver(contract.Driver(value.Driver)) {
			return fmt.Errorf("invalid Jangolova driver %q", value.Driver)
		}
	}
	value.Policy.AllowedCapabilities = stableStrings(value.Policy.AllowedCapabilities)
	value.Policy.AllowedOrigins = stableStrings(value.Policy.AllowedOrigins)
	value.Policy.AllowedBundleIDs = stableStrings(value.Policy.AllowedBundleIDs)
	for _, pattern := range value.Policy.AllowedOrigins {
		if err := validateOriginPattern(pattern); err != nil {
			return err
		}
	}
	for _, bundleID := range value.Policy.AllowedBundleIDs {
		if !macOSBundleIDPattern.MatchString(bundleID) {
			return fmt.Errorf("invalid Jangolova allowed bundle ID %q", bundleID)
		}
	}
	return nil
}

func validateOriginPattern(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid Jangolova allowed origin %q", value)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "*" {
		return fmt.Errorf("Jangolova allowed origin %q has unsupported scheme", value)
	}
	return nil
}

func capabilityAllowed(allowed []string, name string) bool {
	if len(allowed) == 0 {
		return true
	}
	index := sort.SearchStrings(allowed, name)
	return index < len(allowed) && allowed[index] == name
}

func originAllowed(patterns []string, rawURL string) bool {
	if len(patterns) == 0 || rawURL == "" {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	for _, pattern := range patterns {
		candidate, _ := url.Parse(pattern)
		if candidate.Scheme != "*" && candidate.Scheme != parsed.Scheme {
			continue
		}
		matched, _ := path.Match(candidate.Host, parsed.Host)
		if matched {
			return true
		}
	}
	return false
}
