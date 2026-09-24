package webpresentation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const maximumConfigurableArtifactBytes = 32 * 1024 * 1024
const maximumActionTimeoutMillis = 120000

type policyConfig struct {
	MaxHTMLBytes              int      `json:"maxHTMLBytes,omitempty"`
	MaxCSSBytes               int      `json:"maxCSSBytes,omitempty"`
	MaxJavaScriptBytes        int      `json:"maxJavaScriptBytes,omitempty"`
	MaxTotalBytes             int      `json:"maxTotalBytes,omitempty"`
	AllowedSourceOrigins      []string `json:"allowedSourceOrigins,omitempty"`
	AllowedAssetOrigins       []string `json:"allowedAssetOrigins,omitempty"`
	AllowedArtifactTransports []string `json:"allowedArtifactTransports,omitempty"`
	AuthorizedActions         []string `json:"authorizedActions,omitempty"`
	ExecuteTimeoutMillis      int      `json:"executeTimeoutMillis,omitempty"`
	CaptureTimeoutMillis      int      `json:"captureTimeoutMillis,omitempty"`
	MountTimeoutMillis        int      `json:"mountTimeoutMillis,omitempty"`
}

type presentationPolicy struct {
	MaxHTMLBytes              int      `json:"maxHTMLBytes"`
	MaxCSSBytes               int      `json:"maxCSSBytes"`
	MaxJavaScriptBytes        int      `json:"maxJavaScriptBytes"`
	MaxTotalBytes             int      `json:"maxTotalBytes"`
	AllowedSourceOrigins      []string `json:"allowedSourceOrigins,omitempty"`
	AllowedAssetOrigins       []string `json:"allowedAssetOrigins"`
	AllowedArtifactTransports []string `json:"allowedArtifactTransports"`
	AuthorizedActions         []string `json:"authorizedActions"`
	ExecuteTimeoutMillis      int      `json:"executeTimeoutMillis"`
	CaptureTimeoutMillis      int      `json:"captureTimeoutMillis"`
	MountTimeoutMillis        int      `json:"mountTimeoutMillis"`
}

func defaultPresentationPolicy() presentationPolicy {
	return presentationPolicy{
		MaxHTMLBytes:              1024 * 1024,
		MaxCSSBytes:               256 * 1024,
		MaxJavaScriptBytes:        256 * 1024,
		MaxTotalBytes:             1536 * 1024,
		AllowedAssetOrigins:       []string{"self", "data:", "blob:"},
		AllowedArtifactTransports: []string{"http", "https"},
		AuthorizedActions:         []string{"presentation.capture", "presentation.execute", "presentation.mount"},
		ExecuteTimeoutMillis:      5000,
		CaptureTimeoutMillis:      10000,
		MountTimeoutMillis:        15000,
	}
}

func resolvePolicy(config policyConfig) (presentationPolicy, error) {
	policy := defaultPresentationPolicy()
	for name, configured := range map[string]struct {
		value  int
		assign func(int)
	}{
		"maxHTMLBytes":       {config.MaxHTMLBytes, func(value int) { policy.MaxHTMLBytes = value }},
		"maxCSSBytes":        {config.MaxCSSBytes, func(value int) { policy.MaxCSSBytes = value }},
		"maxJavaScriptBytes": {config.MaxJavaScriptBytes, func(value int) { policy.MaxJavaScriptBytes = value }},
		"maxTotalBytes":      {config.MaxTotalBytes, func(value int) { policy.MaxTotalBytes = value }},
	} {
		if configured.value == 0 {
			continue
		}
		if configured.value < 0 || configured.value > maximumConfigurableArtifactBytes {
			return presentationPolicy{}, fmt.Errorf("presentation policy %s must be between 1 and %d", name, maximumConfigurableArtifactBytes)
		}
		configured.assign(configured.value)
	}

	var err error
	policy.AllowedSourceOrigins, err = normalizeOriginList(config.AllowedSourceOrigins, false)
	if err != nil {
		return presentationPolicy{}, fmt.Errorf("presentation policy allowedSourceOrigins: %w", err)
	}
	if len(config.AllowedAssetOrigins) > 0 {
		policy.AllowedAssetOrigins, err = normalizeOriginList(config.AllowedAssetOrigins, true)
		if err != nil {
			return presentationPolicy{}, fmt.Errorf("presentation policy allowedAssetOrigins: %w", err)
		}
	}
	if len(config.AllowedArtifactTransports) > 0 {
		policy.AllowedArtifactTransports, err = normalizeArtifactTransports(config.AllowedArtifactTransports)
		if err != nil {
			return presentationPolicy{}, fmt.Errorf("presentation policy allowedArtifactTransports: %w", err)
		}
	}
	if config.AuthorizedActions != nil {
		policy.AuthorizedActions, err = normalizeAuthorizedActions(config.AuthorizedActions)
		if err != nil {
			return presentationPolicy{}, fmt.Errorf("presentation policy authorizedActions: %w", err)
		}
	}
	if config.ExecuteTimeoutMillis != 0 {
		if err := validateActionTimeout("executeTimeoutMillis", config.ExecuteTimeoutMillis); err != nil {
			return presentationPolicy{}, err
		}
		policy.ExecuteTimeoutMillis = config.ExecuteTimeoutMillis
	}
	if config.CaptureTimeoutMillis != 0 {
		if err := validateActionTimeout("captureTimeoutMillis", config.CaptureTimeoutMillis); err != nil {
			return presentationPolicy{}, err
		}
		policy.CaptureTimeoutMillis = config.CaptureTimeoutMillis
	}
	if config.MountTimeoutMillis != 0 {
		if err := validateActionTimeout("mountTimeoutMillis", config.MountTimeoutMillis); err != nil {
			return presentationPolicy{}, err
		}
		policy.MountTimeoutMillis = config.MountTimeoutMillis
	}
	return policy, nil
}

func normalizeArtifactTransports(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value != "http" && value != "https" && value != "target-file" {
			return nil, fmt.Errorf("unsupported web presentation artifact transport %q", value)
		}
		if !stringAllowed(value, result) {
			result = append(result, value)
		}
	}
	return result, nil
}

func normalizeOriginList(values []string, allowSpecial bool) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if allowSpecial && (value == "self" || value == "data:" || value == "blob:") {
			// Keep provider-neutral asset rules explicit rather than expanding
			// "self" before the caller-owned page is selected.
		} else {
			parsed, err := url.Parse(value)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
				return nil, fmt.Errorf("%q must be an HTTP(S) origin without a path, query, fragment, or user info", value)
			}
			value = strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func (p presentationPolicy) validateSource(source string) error {
	if strings.TrimSpace(source) == "" || len(p.AllowedSourceOrigins) == 0 {
		return nil
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" {
		return errors.New("presentation source must have an origin when allowedSourceOrigins is configured")
	}
	origin := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
	for _, allowed := range p.AllowedSourceOrigins {
		if origin == allowed {
			return nil
		}
	}
	return fmt.Errorf("presentation source origin %q is not allowed", origin)
}

func normalizeAuthorizedActions(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		switch value {
		case "presentation.capture", "presentation.execute", "presentation.mount":
		default:
			return nil, fmt.Errorf("%q is not a sensitive presentation action", value)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func validateActionTimeout(name string, value int) error {
	if value < 1 || value > maximumActionTimeoutMillis {
		return fmt.Errorf("presentation policy %s must be between 1 and %d", name, maximumActionTimeoutMillis)
	}
	return nil
}

func (p presentationPolicy) actionAuthorized(name string) bool {
	if name != "presentation.capture" && name != "presentation.execute" && name != "presentation.mount" {
		return true
	}
	for _, allowed := range p.AuthorizedActions {
		if allowed == name {
			return true
		}
	}
	return false
}

func (p presentationPolicy) validateCall(method string, params json.RawMessage) error {
	if method != "act" {
		return nil
	}
	var call struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return fmt.Errorf("decode presentation action for policy: %w", err)
	}
	if !p.actionAuthorized(call.Name) {
		return fmt.Errorf("%s is not authorized by presentation policy", call.Name)
	}
	switch call.Name {
	case "presentation.write":
		return p.validateSourceInput(call.Input)
	case "presentation.execute":
		var input struct {
			Code string `json:"code"`
		}
		if err := strictDecode(call.Input, &input); err != nil {
			return fmt.Errorf("decode presentation.execute input: %w", err)
		}
		return enforceByteLimit("presentation JavaScript", len(input.Code), p.MaxJavaScriptBytes)
	case "presentation.create", "presentation.replace":
		var input struct {
			Document json.RawMessage `json:"document"`
		}
		if err := json.Unmarshal(call.Input, &input); err != nil {
			return fmt.Errorf("decode %s input: %w", call.Name, err)
		}
		if err := enforceByteLimit("presentation document", len(bytes.TrimSpace(input.Document)), p.MaxTotalBytes); err != nil {
			return err
		}
		var documentFields map[string]json.RawMessage
		if err := json.Unmarshal(input.Document, &documentFields); err == nil && documentFields["html"] != nil {
			return p.validateSourceInput(input.Document)
		}
	case "presentation.patch":
		return enforceByteLimit("presentation patch", len(bytes.TrimSpace(call.Input)), p.MaxTotalBytes)
	case "presentation.mount":
		return p.validateArtifactMount(call.Input)
	}
	return nil
}

func (p presentationPolicy) validateSourceInput(raw json.RawMessage) error {
	var input struct {
		HTML string `json:"html"`
		CSS  string `json:"css"`
		JS   string `json:"js"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Errorf("decode presentation source: %w", err)
	}
	if err := enforceByteLimit("presentation HTML", len(input.HTML), p.MaxHTMLBytes); err != nil {
		return err
	}
	if err := enforceByteLimit("presentation CSS", len(input.CSS), p.MaxCSSBytes); err != nil {
		return err
	}
	if err := enforceByteLimit("presentation JavaScript", len(input.JS), p.MaxJavaScriptBytes); err != nil {
		return err
	}
	return enforceByteLimit("total presentation source", len(input.HTML)+len(input.CSS)+len(input.JS), p.MaxTotalBytes)
}

func enforceByteLimit(name string, actual, maximum int) error {
	if actual > maximum {
		return fmt.Errorf("%s is %d bytes; policy allows at most %d", name, actual, maximum)
	}
	return nil
}

func strictDecode(raw json.RawMessage, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}
