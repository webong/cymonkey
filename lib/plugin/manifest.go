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

const APIVersion = "cymonkey.plugin/v1alpha1"

const (
	JangolovaEngine  = "jangolova.engine"
	BlockadeProvider = "blockade.provider"
	BoardProvider    = "board.provider"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var versionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)

// Manifest names one reviewed executable. Command is a filename in the same
// directory, never a shell command or a path outside that directory.
type Manifest struct {
	APIVersion string `json:"apiVersion"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Platform   string `json:"platform,omitempty"`
	Command    string `json:"command"`
	SHA256     string `json:"sha256"`
}

func (m Manifest) Validate() error {
	if m.APIVersion != APIVersion {
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
	switch m.Kind {
	case JangolovaEngine, BlockadeProvider, BoardProvider:
	default:
		return errors.New("plugin kind is invalid")
	}
	if m.Command == "" || m.Command == "." || m.Command == ".." || m.Command == "plugin.json" || filepath.Base(m.Command) != m.Command || strings.ContainsAny(m.Command, `/\\`) {
		return errors.New("plugin command must be a filename")
	}
	if !digestPattern.MatchString(m.SHA256) {
		return errors.New("plugin sha256 must be 64 lowercase hexadecimal characters")
	}
	return nil
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
