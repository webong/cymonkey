// Package providerplugin implements the transport and installation boundary for
// optional executable providers. It contains no domain-specific adapter code.
package providerplugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const APIVersion = "provider.plugin/v1alpha1"
const legacyAPIVersion = "cymonkey.plugin/v1alpha1"

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)+$`)
var contractVersionPattern = regexp.MustCompile(`^v[0-9]+(?:(?:alpha|beta)[0-9]+)?$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var versionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)

// Manifest names one reviewed executable. Command is a filename in the same
// directory, never a shell command or a path outside that directory.
type Manifest struct {
	APIVersion string     `json:"apiVersion"`
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	Contracts  []Contract `json:"contracts,omitempty"`
	// Kind is accepted for compatibility with the original single-contract
	// manifest. New packages declare versioned Contracts.
	Kind     string `json:"kind,omitempty"`
	Platform string `json:"platform,omitempty"`
	Command  string `json:"command"`
	SHA256   string `json:"sha256"`
}

type Contract struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (m Manifest) Validate() error {
	if m.APIVersion != APIVersion && m.APIVersion != legacyAPIVersion {
		return fmt.Errorf("plugin apiVersion must be %q", APIVersion)
	}
	if !namePattern.MatchString(m.Name) {
		return errors.New("plugin name is invalid")
	}
	if !versionPattern.MatchString(m.Version) {
		return errors.New("plugin version must be a semantic version")
	}
	if m.Platform != "" && m.Platform != runtime.GOOS+"-"+runtime.GOARCH {
		return errors.New("plugin platform does not match this host")
	}
	if (m.Kind == "") == (len(m.Contracts) == 0) {
		return errors.New("plugin must declare contracts or one legacy kind")
	}
	if m.Kind != "" && (len(m.Kind) > 128 || !kindPattern.MatchString(m.Kind)) {
		return errors.New("plugin kind must be a dotted lower-case identifier")
	}
	if len(m.Contracts) > 16 {
		return errors.New("plugin cannot declare more than 16 contracts")
	}
	seenContracts := make(map[string]bool, len(m.Contracts))
	for _, contract := range m.Contracts {
		key := contract.Name + "@" + contract.Version
		if len(contract.Name) > 128 || !kindPattern.MatchString(contract.Name) || !contractVersionPattern.MatchString(contract.Version) || seenContracts[key] {
			return errors.New("plugin contract name, version, or uniqueness is invalid")
		}
		seenContracts[key] = true
	}
	if m.Command == "" || m.Command == "." || m.Command == ".." || m.Command == "plugin.json" || filepath.Base(m.Command) != m.Command || strings.ContainsAny(m.Command, `/\\`) {
		return errors.New("plugin command must be a filename")
	}
	if !digestPattern.MatchString(m.SHA256) {
		return errors.New("plugin sha256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}

// Supports reports whether this package implements a named, versioned public
// contract. Legacy kind manifests match the name for v1alpha1.
func (m Manifest) Supports(name, version string) bool {
	if m.Kind != "" {
		return m.Kind == name && version == "v1alpha1"
	}
	for _, contract := range m.Contracts {
		if contract.Name == name && contract.Version == version {
			return true
		}
	}
	return false
}

func sameContractIdentity(aKind string, aContracts []Contract, bKind string, bContracts []Contract) bool {
	if aKind != bKind || len(aContracts) != len(bContracts) {
		return false
	}
	remaining := make(map[string]int, len(aContracts))
	for _, contract := range aContracts {
		remaining[contract.Name+"@"+contract.Version]++
	}
	for _, contract := range bContracts {
		key := contract.Name + "@" + contract.Version
		remaining[key]--
		if remaining[key] < 0 {
			return false
		}
	}
	return true
}

func DecodeManifest(reader io.Reader) (Manifest, error) {
	var m Manifest
	data, err := io.ReadAll(io.LimitReader(reader, (64<<10)+1))
	if err != nil {
		return m, err
	}
	if len(data) > 64<<10 {
		return m, errors.New("plugin manifest exceeds 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return m, errors.New("plugin manifest must contain one JSON object")
	}
	return m, m.Validate()
}

func LoadManifest(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	return DecodeManifest(f)
}

// Installed is one local plugin whose executable digest has been verified.
type Installed struct {
	Manifest  Manifest
	Directory string
}

func (p Installed) executable() (string, error) {
	if err := p.Manifest.Validate(); err != nil {
		return "", err
	}
	path := filepath.Join(p.Directory, p.Manifest.Command)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("plugin executable must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, (128<<20)+1))
	if err != nil {
		return "", err
	}
	if n > 128<<20 {
		return "", errors.New("plugin executable exceeds 128 MiB")
	}
	if hex.EncodeToString(h.Sum(nil)) != p.Manifest.SHA256 {
		return "", errors.New("plugin executable digest mismatch")
	}
	return path, nil
}

// List loads only explicitly installed directories and re-verifies every
// executable. A bad installation fails closed instead of being silently used.
func List(root string) ([]Installed, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plugins := make([]Installed, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		m, err := LoadManifest(filepath.Join(root, entry.Name(), "plugin.json"))
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", entry.Name(), err)
		}
		if m.Name != entry.Name() {
			return nil, fmt.Errorf("plugin directory %s does not match manifest", entry.Name())
		}
		p := Installed{Manifest: m, Directory: filepath.Join(root, entry.Name())}
		if _, err := p.executable(); err != nil {
			return nil, fmt.Errorf("plugin %s: %w", m.Name, err)
		}
		plugins = append(plugins, p)
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Manifest.Name < plugins[j].Manifest.Name })
	return plugins, nil
}
