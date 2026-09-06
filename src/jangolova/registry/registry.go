// Package registry discovers and retrieves reviewed Jangolova
// runtime modules. Discovery is metadata-only; callers must explicitly select
// and mount a module after policy approval.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const SchemaVersion = "jangolova.registry/v1alpha1"

var moduleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(/[a-z0-9][a-z0-9-]*)+$`)
var digestPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

// Registry is the reviewed metadata index exposed by a Jangolova registry
// service. It intentionally contains no executable code.
type Registry struct {
	Schema        string   `json:"$schema,omitempty"`
	SchemaVersion string   `json:"schemaVersion"`
	RegistryID    string   `json:"registryId"`
	Modules       []Module `json:"modules"`
}

type Module struct {
	ID              string     `json:"id"`
	Version         string     `json:"version"`
	Runtime         string     `json:"runtime"`
	ProtocolVersion string     `json:"protocolVersion"`
	Status          string     `json:"status"`
	Platforms       []string   `json:"platforms,omitempty"`
	Actions         []string   `json:"actions,omitempty"`
	Artifacts       []Artifact `json:"artifacts,omitempty"`
}

type Artifact struct {
	Platform  string `json:"platform"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

type DiscoveryOptions struct {
	HTTPClient *http.Client
}

type PullOptions struct {
	HTTPClient *http.Client
}

func Decode(r io.Reader) (Registry, error) {
	var registry Registry
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return Registry{}, fmt.Errorf("decode Jangolova registry: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Registry{}, errors.New("Jangolova registry must contain exactly one JSON document")
	}
	if err := registry.Validate(); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

func LoadFile(path string) (Registry, error) {
	file, err := os.Open(path)
	if err != nil {
		return Registry{}, err
	}
	defer file.Close()
	return Decode(file)
}

// Discover fetches registry metadata. Plain HTTP is accepted only for a
// loopback endpoint, making local fixtures convenient without weakening
// production transport requirements.
func Discover(ctx context.Context, endpoint string, options DiscoveryOptions) (Registry, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return Registry{}, errors.New("Jangolova registry endpoint must be an HTTP(S) URL")
	}
	if parsed.Scheme == "http" && !isLoopback(parsed.Hostname()) {
		return Registry{}, errors.New("Jangolova registry requires HTTPS outside loopback")
	}
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Registry{}, fmt.Errorf("create Jangolova registry request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return Registry{}, fmt.Errorf("discover Jangolova registry: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Registry{}, fmt.Errorf("discover Jangolova registry: HTTP %s", response.Status)
	}
	return Decode(io.LimitReader(response.Body, 8<<20))
}

func (r Registry) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("Jangolova registry schemaVersion must be %q", SchemaVersion)
	}
	if strings.TrimSpace(r.RegistryID) == "" {
		return errors.New("Jangolova registry registryId is required")
	}
	if len(r.Modules) == 0 {
		return errors.New("Jangolova registry must contain at least one module")
	}
	seen := make(map[string]struct{}, len(r.Modules))
	for _, module := range r.Modules {
		if !moduleIDPattern.MatchString(module.ID) {
			return fmt.Errorf("Jangolova module id %q is invalid", module.ID)
		}
		if strings.TrimSpace(module.Version) == "" || strings.TrimSpace(module.Runtime) == "" {
			return fmt.Errorf("Jangolova module %q requires version and runtime", module.ID)
		}
		if strings.TrimSpace(module.ProtocolVersion) == "" {
			return fmt.Errorf("Jangolova module %q requires protocolVersion", module.ID)
		}
		switch module.Status {
		case "available", "deprecated", "revoked":
		default:
			return fmt.Errorf("Jangolova module %q has unsupported status %q", module.ID, module.Status)
		}
		key := module.ID + "@" + module.Version
		if _, exists := seen[key]; exists {
			return fmt.Errorf("Jangolova module %q is duplicated", key)
		}
		seen[key] = struct{}{}
		for _, artifact := range module.Artifacts {
			if strings.TrimSpace(artifact.Platform) == "" || strings.TrimSpace(artifact.URL) == "" {
				return fmt.Errorf("Jangolova module %q has an incomplete artifact", module.ID)
			}
			parsed, err := url.Parse(artifact.URL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
				return fmt.Errorf("Jangolova module %q has an invalid artifact URL", module.ID)
			}
			if !digestPattern.MatchString(artifact.SHA256) {
				return fmt.Errorf("Jangolova module %q artifact sha256 must be 64 hexadecimal characters", module.ID)
			}
		}
	}
	return nil
}

func (r Registry) Select(moduleID, platform string) (Module, error) {
	for _, module := range r.Modules {
		if module.ID != moduleID {
			continue
		}
		if module.Status == "revoked" {
			return Module{}, fmt.Errorf("Jangolova module %q is revoked", moduleID)
		}
		if platform == "" || contains(module.Platforms, platform) {
			return module, nil
		}
		return Module{}, fmt.Errorf("Jangolova module %q does not support platform %q", moduleID, platform)
	}
	return Module{}, fmt.Errorf("Jangolova module %q was not discovered", moduleID)
}

// Pull downloads one selected artifact into destination and verifies its
// SHA-256 digest before making it visible. It never executes or mounts the
// downloaded file.
func Pull(ctx context.Context, module Module, platform, destination string, options PullOptions) (string, error) {
	if module.Status == "revoked" {
		return "", fmt.Errorf("Jangolova module %q is revoked", module.ID)
	}
	if platform == "" || !contains(module.Platforms, platform) {
		return "", fmt.Errorf("Jangolova module %q does not support platform %q", module.ID, platform)
	}
	var selected *Artifact
	for index := range module.Artifacts {
		if module.Artifacts[index].Platform == platform {
			selected = &module.Artifacts[index]
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("Jangolova module %q has no artifact for platform %q", module.ID, platform)
	}
	parsed, err := url.Parse(selected.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("Jangolova artifact URL must be an HTTP(S) URL")
	}
	if parsed.Scheme == "http" && !isLoopback(parsed.Hostname()) {
		return "", errors.New("Jangolova artifact requires HTTPS outside loopback")
	}
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, selected.URL, nil)
	if err != nil {
		return "", fmt.Errorf("create Jangolova artifact request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("pull Jangolova module %q: %w", module.ID, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("pull Jangolova module %q: HTTP %s", module.ID, response.Status)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", fmt.Errorf("create Jangolova module cache: %w", err)
	}
	name := filepath.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		name = strings.ReplaceAll(module.ID, "/", "-") + "-" + module.Version
	}
	temporary, err := os.CreateTemp(destination, ".jangolova-download-*")
	if err != nil {
		return "", fmt.Errorf("create Jangolova module cache file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), response.Body); err != nil {
		temporary.Close()
		return "", fmt.Errorf("download Jangolova module %q: %w", module.ID, err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close Jangolova module cache file: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, selected.SHA256) {
		return "", fmt.Errorf("Jangolova module %q digest mismatch: got %s, want %s", module.ID, actual, selected.SHA256)
	}
	finalPath := filepath.Join(destination, name)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", fmt.Errorf("publish Jangolova module %q: %w", module.ID, err)
	}
	return finalPath, nil
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
