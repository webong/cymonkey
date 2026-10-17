package contentengine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"jangolova/contentkit"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const RuntimeManifestAPIVersion = "content.runtime/v1"

const maxRuntimeManifestBytes = 16 << 20
const maxRuntimeFileBytes int64 = 4 << 30

// RuntimeManifest describes a complete local runtime distribution owned by
// Blockade. Packaging tools list every regular file so installation and every
// later resolution can fail closed on tampering.
type RuntimeManifest struct {
	APIVersion string        `json:"apiVersion"`
	Name       string        `json:"name"`
	Version    string        `json:"version"`
	Platform   string        `json:"platform"`
	Executable string        `json:"executable"`
	Files      []RuntimeFile `json:"files"`
}

type RuntimeFile struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable,omitempty"`
}

type InstalledRuntime struct {
	Manifest  RuntimeManifest `json:"manifest"`
	Directory string          `json:"directory"`
}

func (m RuntimeManifest) Validate() error {
	if m.APIVersion != RuntimeManifestAPIVersion {
		return fmt.Errorf("runtime apiVersion must be %q", RuntimeManifestAPIVersion)
	}
	if !contentkit.ModelNamePattern.MatchString(m.Name) || !contentkit.ModelVersionPattern.MatchString(m.Version) {
		return errors.New("runtime name or version is invalid")
	}
	if m.Platform != runtime.GOOS+"-"+runtime.GOARCH {
		return errors.New("runtime platform does not match this host")
	}
	if !validRuntimePath(m.Executable) {
		return errors.New("runtime executable path is invalid")
	}
	if len(m.Files) == 0 || len(m.Files) > 100000 {
		return errors.New("runtime needs 1 to 100000 files")
	}
	seen := make(map[string]bool, len(m.Files))
	executableFound := false
	for _, file := range m.Files {
		if !validRuntimePath(file.Path) || seen[file.Path] {
			return errors.New("runtime file path or uniqueness is invalid")
		}
		if !contentkit.ModelDigestPattern.MatchString(file.SHA256) {
			return fmt.Errorf("runtime file %q needs a lowercase SHA-256 digest", file.Path)
		}
		seen[file.Path] = true
		if file.Path == m.Executable {
			executableFound = file.Executable
		}
	}
	if !executableFound {
		return errors.New("runtime executable must be declared as an executable file")
	}
	return nil
}

func validRuntimePath(value string) bool {
	return value != "" && value != "." && value != ".." && value != "runtime.json" &&
		!strings.Contains(value, `\`) && !strings.HasPrefix(value, "/") &&
		pathpkg.Clean(value) == value && !strings.HasPrefix(value, "../")
}

func LoadRuntimeManifest(path string) (RuntimeManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return RuntimeManifest{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRuntimeManifestBytes+1))
	if err != nil {
		return RuntimeManifest{}, err
	}
	if len(data) > maxRuntimeManifestBytes {
		return RuntimeManifest{}, errors.New("runtime manifest exceeds 16 MiB")
	}
	var manifest RuntimeManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return RuntimeManifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return RuntimeManifest{}, errors.New("runtime manifest must contain one JSON object")
	}
	// Manifests installed before the neutral names keep loading for one release.
	if manifest.APIVersion == "blockade.runtime/v1alpha1" || manifest.APIVersion == "cymonkey.runtime/v1alpha1" {
		manifest.APIVersion = RuntimeManifestAPIVersion
	}
	return manifest, manifest.Validate()
}

func DefaultRuntimeRoot() (string, error) {
	if root := os.Getenv("BLOCKADE_RUNTIME_DIR"); root != "" {
		return filepath.Abs(root)
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "blockade", "runtimes"), nil
}

// BuildRuntimeManifest inventories a self-contained runtime tree for review
// and later installation. It does not modify or install the source tree.
func BuildRuntimeManifest(source, name, version, executable string) (RuntimeManifest, error) {
	if !validRuntimePath(executable) {
		return RuntimeManifest{}, errors.New("runtime executable path is invalid")
	}
	root, err := filepath.Abs(source)
	if err != nil {
		return RuntimeManifest{}, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return RuntimeManifest{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return RuntimeManifest{}, errors.New("runtime source must be a directory, not a symbolic link")
	}
	manifest := RuntimeManifest{
		APIVersion: RuntimeManifestAPIVersion,
		Name:       name,
		Version:    version,
		Platform:   runtime.GOOS + "-" + runtime.GOARCH,
		Executable: executable,
	}
	err = filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == root {
			return nil
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "runtime.json" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime source contains symbolic link %q", relative)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxRuntimeFileBytes {
			return fmt.Errorf("runtime source file %q must be a regular file under 4 GiB", relative)
		}
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		hash := sha256.New()
		n, readErr := io.Copy(hash, io.LimitReader(file, maxRuntimeFileBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		if n > maxRuntimeFileBytes {
			return fmt.Errorf("runtime source file %q exceeds 4 GiB", relative)
		}
		manifest.Files = append(manifest.Files, RuntimeFile{
			Path:       relative,
			SHA256:     hex.EncodeToString(hash.Sum(nil)),
			Executable: info.Mode().Perm()&0111 != 0,
		})
		if len(manifest.Files) > 100000 {
			return errors.New("runtime source contains more than 100000 files")
		}
		return nil
	})
	if err != nil {
		return RuntimeManifest{}, err
	}
	return manifest, manifest.Validate()
}

// InstallRuntime copies a complete, locally reviewed runtime distribution into
// Blockade's registry. Symbolic links are rejected; distribution builders must
// materialize their targets in the package.
func InstallRuntime(root, manifestPath string) (InstalledRuntime, error) {
	manifest, err := LoadRuntimeManifest(manifestPath)
	if err != nil {
		return InstalledRuntime{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return InstalledRuntime{}, err
	}
	nameRoot := filepath.Join(root, manifest.Name)
	if err := os.MkdirAll(nameRoot, 0700); err != nil {
		return InstalledRuntime{}, err
	}
	destination := filepath.Join(nameRoot, manifest.Version)
	if _, err := os.Lstat(destination); err == nil {
		return InstalledRuntime{}, fmt.Errorf("runtime %s@%s is already installed", manifest.Name, manifest.Version)
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstalledRuntime{}, err
	}
	staging, err := os.MkdirTemp(nameRoot, ".install-")
	if err != nil {
		return InstalledRuntime{}, err
	}
	defer os.RemoveAll(staging)
	sourceRoot := filepath.Dir(manifestPath)
	for _, file := range manifest.Files {
		mode := os.FileMode(0600)
		if file.Executable {
			mode = 0700
		}
		if err := copyRuntimeFile(sourceRoot, staging, file, mode); err != nil {
			return InstalledRuntime{}, fmt.Errorf("install runtime file %q: %w", file.Path, err)
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return InstalledRuntime{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "runtime.json"), append(encoded, '\n'), 0600); err != nil {
		return InstalledRuntime{}, err
	}
	staged := InstalledRuntime{Manifest: manifest, Directory: staging}
	if err := verifyRuntime(staged); err != nil {
		return InstalledRuntime{}, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return InstalledRuntime{}, err
	}
	return InstalledRuntime{Manifest: manifest, Directory: destination}, nil
}

func copyRuntimeFile(sourceRoot, destinationRoot string, file RuntimeFile, mode os.FileMode) error {
	source, err := secureRuntimeFile(sourceRoot, file.Path)
	if err != nil {
		return err
	}
	destination := filepath.Join(destinationRoot, filepath.FromSlash(file.Path))
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(output, io.TeeReader(io.LimitReader(input, maxRuntimeFileBytes+1), hash))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if n > maxRuntimeFileBytes || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return errors.New("runtime file changed or digest mismatched")
	}
	return nil
}

func secureRuntimeFile(root, name string) (string, error) {
	if !validRuntimePath(name) {
		return "", errors.New("runtime path is invalid")
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
			return "", errors.New("runtime paths cannot contain symbolic links")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", errors.New("runtime parent must be a directory")
		}
		if index == len(parts)-1 && (!info.Mode().IsRegular() || info.Size() > maxRuntimeFileBytes) {
			return "", errors.New("runtime payload must be a regular file under 4 GiB")
		}
	}
	return current, nil
}

func verifyRuntime(installed InstalledRuntime) error {
	if err := installed.Manifest.Validate(); err != nil {
		return err
	}
	for _, declared := range installed.Manifest.Files {
		path, err := secureRuntimeFile(installed.Directory, declared.Path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		n, readErr := io.Copy(hash, io.LimitReader(file, maxRuntimeFileBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		if n > maxRuntimeFileBytes || hex.EncodeToString(hash.Sum(nil)) != declared.SHA256 {
			return fmt.Errorf("runtime file %q digest mismatch", declared.Path)
		}
	}
	return nil
}

func ResolveInstalledRuntime(root, name, version string) (InstalledRuntime, error) {
	if !contentkit.ModelNamePattern.MatchString(name) || !contentkit.ModelVersionPattern.MatchString(version) {
		return InstalledRuntime{}, errors.New("runtime reference must be a valid name and version")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return InstalledRuntime{}, err
	}
	directory := filepath.Join(root, name, version)
	info, err := os.Lstat(directory)
	if err != nil {
		return InstalledRuntime{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return InstalledRuntime{}, errors.New("installed runtime directory is invalid")
	}
	manifest, err := LoadRuntimeManifest(filepath.Join(directory, "runtime.json"))
	if err != nil {
		return InstalledRuntime{}, err
	}
	if manifest.Name != name || manifest.Version != version {
		return InstalledRuntime{}, errors.New("installed runtime manifest does not match its directory")
	}
	installed := InstalledRuntime{Manifest: manifest, Directory: directory}
	if err := verifyRuntime(installed); err != nil {
		return InstalledRuntime{}, err
	}
	return installed, nil
}

func (r InstalledRuntime) ExecutablePath() string {
	return filepath.Join(r.Directory, filepath.FromSlash(r.Manifest.Executable))
}

func ListInstalledRuntimes(root string) ([]InstalledRuntime, error) {
	names, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []InstalledRuntime{}, nil
	}
	if err != nil {
		return nil, err
	}
	var installed []InstalledRuntime
	for _, name := range names {
		if !name.IsDir() || !contentkit.ModelNamePattern.MatchString(name.Name()) {
			continue
		}
		versions, err := os.ReadDir(filepath.Join(root, name.Name()))
		if err != nil {
			return nil, err
		}
		for _, version := range versions {
			if !version.IsDir() || !contentkit.ModelVersionPattern.MatchString(version.Name()) {
				continue
			}
			item, err := ResolveInstalledRuntime(root, name.Name(), version.Name())
			if err != nil {
				return nil, fmt.Errorf("installed runtime %s@%s: %w", name.Name(), version.Name(), err)
			}
			installed = append(installed, item)
		}
	}
	slices.SortFunc(installed, func(a, b InstalledRuntime) int {
		return strings.Compare(a.Manifest.Name+"@"+a.Manifest.Version, b.Manifest.Name+"@"+b.Manifest.Version)
	})
	return installed, nil
}

func RemoveInstalledRuntime(root, name, version string) error {
	installed, err := ResolveInstalledRuntime(root, name, version)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(installed.Directory); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(installed.Directory))
	return nil
}
