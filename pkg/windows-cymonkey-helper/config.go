package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// helperConfig contains the owner-selected desktop scope.  It intentionally
// has no executable path, shell command, endpoint, or credential fields.
// Launch material is supplied independently in the environment.
type helperConfig struct {
	AllowedExecutableNames []string      `json:"allowedExecutableNames"`
	Viewer                 *viewerPolicy `json:"viewer,omitempty"`
}

type viewerPolicy struct {
	Enabled          bool     `json:"enabled"`
	AllowCapture     bool     `json:"allowCapture"`
	AllowInput       bool     `json:"allowInput"`
	MaxTextLength    int      `json:"maxTextLength,omitempty"`
	BlockedKeys      []string `json:"blockedKeys,omitempty"`
	RedactTypedInput bool     `json:"redactTypedInput,omitempty"`
}

func loadConfig(path string) (helperConfig, error) {
	if !filepath.IsAbs(path) {
		return helperConfig{}, errors.New("Cymonkey helper configuration path must be absolute")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return helperConfig{}, errors.New("read Cymonkey helper configuration")
	}
	var config helperConfig
	if err := json.Unmarshal(contents, &config); err != nil {
		return helperConfig{}, errors.New("decode Cymonkey helper configuration")
	}
	if err := config.validate(); err != nil {
		return helperConfig{}, err
	}
	return config, nil
}

func (c *helperConfig) validate() error {
	seen := map[string]struct{}{}
	allowed := make([]string, 0, len(c.AllowedExecutableNames))
	for _, raw := range c.AllowedExecutableNames {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" || filepath.Base(name) != name || !strings.HasSuffix(name, ".exe") || strings.ContainsAny(name, `\\/:*?"<>|`) {
			return errors.New("allowedExecutableNames must contain Windows executable file names")
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			allowed = append(allowed, name)
		}
	}
	if len(allowed) == 0 {
		return errors.New("at least one allowed executable is required")
	}
	sort.Strings(allowed)
	c.AllowedExecutableNames = allowed

	if c.Viewer != nil {
		if c.Viewer.MaxTextLength < 0 || c.Viewer.MaxTextLength > 10_000 {
			return errors.New("viewer maxTextLength must be between 0 and 10000")
		}
		blocked := map[string]struct{}{}
		for _, raw := range c.Viewer.BlockedKeys {
			key := canonicalKey(raw)
			if key == "" {
				return errors.New("viewer blockedKeys contains an invalid key")
			}
			blocked[key] = struct{}{}
		}
		c.Viewer.BlockedKeys = make([]string, 0, len(blocked))
		for key := range blocked {
			c.Viewer.BlockedKeys = append(c.Viewer.BlockedKeys, key)
		}
		sort.Strings(c.Viewer.BlockedKeys)
	}
	return nil
}

func canonicalKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 80 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return value
}

func (c helperConfig) allowsExecutable(name string) bool {
	name = strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	for _, allowed := range c.AllowedExecutableNames {
		if allowed == name {
			return true
		}
	}
	return false
}
