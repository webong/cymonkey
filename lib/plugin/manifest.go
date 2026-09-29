// Package providerplugin implements the transport and installation boundary for
// optional provider packages. It contains no domain-specific adapter code.
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
	pathpkg "path"
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

// Manifest describes one reviewed package. Executable packages set Command and
// SHA256. Source packages declare every payload under Files and leave Command
// empty. The generic package layer does not interpret those payloads.
type Manifest struct {
	APIVersion string     `json:"apiVersion"`
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	Contracts  []Contract `json:"contracts,omitempty"`
	// Kind is accepted for compatibility with the original single-contract
	// manifest. New packages declare versioned Contracts.
	Kind     string        `json:"kind,omitempty"`
	Platform string        `json:"platform,omitempty"`
	Command  string        `json:"command,omitempty"`
	SHA256   string        `json:"sha256,omitempty"`
	Files    []PackageFile `json:"files,omitempty"`
}

type PackageFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
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
	if (m.Command == "") != (m.SHA256 == "") {
		return errors.New("plugin command and sha256 must be declared together")
	}
	if m.Command != "" {
		if m.Command == "." || m.Command == ".." || m.Command == "plugin.json" || filepath.Base(m.Command) != m.Command || strings.ContainsAny(m.Command, `/\\`) {
			return errors.New("plugin command must be a filename")
		}
		if !digestPattern.MatchString(m.SHA256) {
			return errors.New("plugin sha256 must be 64 lowercase hexadecimal characters")
		}
	}
	if m.Command == "" && len(m.Files) == 0 {
		return errors.New("plugin must declare a command or package files")
	}
	if len(m.Files) > 100000 {
		return errors.New("plugin cannot declare more than 100000 package files")
	}
	seenFiles := make(map[string]bool, len(m.Files)+1)
	seenFiles[m.Command] = m.Command != ""
	for _, file := range m.Files {
		if !validPackagePath(file.Path) || seenFiles[file.Path] {
			return errors.New("plugin package file path or uniqueness is invalid")
		}
		if !digestPattern.MatchString(file.SHA256) {
			return errors.New("plugin package file sha256 must be 64 lowercase hexadecimal characters")
		}
		seenFiles[file.Path] = true
	}
	return nil
}

func validPackagePath(value string) bool {
	return value != "" && value != "." && value != ".." && value != "plugin.json" &&
		!strings.Contains(value, `\`) && !strings.HasPrefix(value, "/") &&
		pathpkg.Clean(value) == value && !strings.HasPrefix(value, "../")
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
	data, err := io.ReadAll(io.LimitReader(reader, (16<<20)+1))
	if err != nil {
		return m, err
	}
	if len(data) > 16<<20 {
		return m, errors.New("plugin manifest exceeds 16 MiB")
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

// Installed is one local package whose declared payload digests can be verified.
type Installed struct {
	Manifest  Manifest
	Directory string
}

func (p Installed) executable() (string, error) {
	if err := p.Manifest.Validate(); err != nil {
		return "", err
	}
	if p.Manifest.Command == "" {
		return "", errors.New("plugin package does not declare an executable")
	}
	return verifyPackageFile(p.Directory, p.Manifest.Command, p.Manifest.SHA256, 128<<20)
}

// Executable returns the verified package command. Source-only packages return
// an error and must be launched by a domain library through their own runtime.
func (p Installed) Executable() (string, error) { return p.executable() }

// Resource returns one verified package file by its manifest path.
func (p Installed) Resource(name string) (string, error) {
	if err := p.Manifest.Validate(); err != nil {
		return "", err
	}
	for _, file := range p.Manifest.Files {
		if file.Path == name {
			return verifyPackageFile(p.Directory, file.Path, file.SHA256, 4<<30)
		}
	}
	return "", fmt.Errorf("plugin package does not declare resource %q", name)
}

// Verify rechecks every declared package payload.
func (p Installed) Verify() error {
	if err := p.Manifest.Validate(); err != nil {
		return err
	}
	if p.Manifest.Command != "" {
		if _, err := p.executable(); err != nil {
			return err
		}
	}
	for _, file := range p.Manifest.Files {
		if _, err := verifyPackageFile(p.Directory, file.Path, file.SHA256, 4<<30); err != nil {
			return fmt.Errorf("package file %s: %w", file.Path, err)
		}
	}
	return nil
}

func verifyPackageFile(root, name, digest string, maximum int64) (string, error) {
	filePath, err := secureRegularFile(root, name)
	if err != nil {
		return "", err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maximum+1))
	if err != nil {
		return "", err
	}
	if n > maximum {
		return "", errors.New("plugin package file exceeds size limit")
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return "", errors.New("plugin package file digest mismatch")
	}
	return filePath, nil
}

func secureRegularFile(root, name string) (string, error) {
	if !validPackagePath(name) {
		return "", errors.New("plugin package path is invalid")
	}
	current := root
	parts := strings.Split(filepath.FromSlash(name), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("plugin package paths cannot contain symbolic links")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", errors.New("plugin package parent must be a directory")
		}
		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return "", errors.New("plugin package payload must be a regular file")
		}
	}
	return current, nil
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
		if err := p.Verify(); err != nil {
			return nil, fmt.Errorf("plugin %s: %w", m.Name, err)
		}
		plugins = append(plugins, p)
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Manifest.Name < plugins[j].Manifest.Name })
	return plugins, nil
}
