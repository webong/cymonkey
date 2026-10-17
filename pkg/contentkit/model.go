package contentkit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const ModelManifestAPIVersion = "content.model/v1"

const maxModelManifestBytes = 64 << 10
const maxModelArtifactBytes int64 = 64 << 30

var modelNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var modelVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)
var modelDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ModelManifest describes files for one local model and the provider that can
// inspect and load them. A provider determines the files' actual model format.
type ModelManifest struct {
	APIVersion   string          `json:"apiVersion"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Provider     string          `json:"provider"`
	Capabilities []string        `json:"capabilities"`
	Artifacts    []ModelArtifact `json:"artifacts"`
}

type ModelArtifact struct {
	Role   string `json:"role"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// InstalledModel is passed to a provider for compatibility checks and at
// startup. Directory is absolute; artifact names are relative filenames.
type InstalledModel struct {
	Manifest  ModelManifest `json:"manifest"`
	Directory string        `json:"directory"`
}

func (m ModelManifest) Validate() error {
	if m.APIVersion != ModelManifestAPIVersion {
		return fmt.Errorf("model apiVersion must be %q", ModelManifestAPIVersion)
	}
	if !modelNamePattern.MatchString(m.Name) || !modelVersionPattern.MatchString(m.Version) || !modelNamePattern.MatchString(m.Provider) {
		return errors.New("model name, version, or provider is invalid")
	}
	if len(m.Capabilities) == 0 || len(m.Capabilities) > 64 {
		return errors.New("model needs 1 to 64 capabilities")
	}
	seenCapabilities := make(map[string]bool, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		if capability == "" || len(capability) > 128 || strings.TrimSpace(capability) != capability || seenCapabilities[capability] {
			return errors.New("model capability is empty, too long, or duplicated")
		}
		seenCapabilities[capability] = true
	}
	if len(m.Artifacts) == 0 || len(m.Artifacts) > 32 {
		return errors.New("model needs 1 to 32 artifacts")
	}
	seenFiles := make(map[string]bool, len(m.Artifacts))
	for _, artifact := range m.Artifacts {
		if !modelNamePattern.MatchString(artifact.Role) {
			return errors.New("model artifact role is invalid")
		}
		if artifact.File == "" || artifact.File == "." || artifact.File == ".." || artifact.File == "model.json" || filepath.Base(artifact.File) != artifact.File || strings.ContainsAny(artifact.File, `/\\`) || seenFiles[artifact.File] {
			return errors.New("model artifact must have a distinct filename")
		}
		if !modelDigestPattern.MatchString(artifact.SHA256) {
			return fmt.Errorf("model artifact %q needs a lowercase SHA-256 digest", artifact.File)
		}
		seenFiles[artifact.File] = true
	}
	return nil
}

func LoadModelManifest(path string) (ModelManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return ModelManifest{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxModelManifestBytes+1))
	if err != nil {
		return ModelManifest{}, err
	}
	if len(data) > maxModelManifestBytes {
		return ModelManifest{}, errors.New("model manifest exceeds 64 KiB")
	}
	var manifest ModelManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return ModelManifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ModelManifest{}, errors.New("model manifest must contain one JSON object")
	}
	// Manifests installed before the neutral names keep loading for one release.
	if manifest.APIVersion == "blockade.model/v1alpha1" || manifest.APIVersion == "cymonkey.model/v1alpha1" {
		manifest.APIVersion = ModelManifestAPIVersion
	}
	return manifest, manifest.Validate()
}

func DefaultModelRoot() (string, error) {
	if root := os.Getenv("BLOCKADE_MODEL_DIR"); root != "" {
		return filepath.Abs(root)
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "blockade", "models"), nil
}

// InstallModel copies a reviewed local model into Blockade's model registry.
// The provider must inspect the staged files before they become available.
func InstallModel(ctx context.Context, root, manifestPath string, inspect func(context.Context, InstalledModel) error) (InstalledModel, error) {
	if inspect == nil {
		return InstalledModel{}, errors.New("model installation requires provider inspection")
	}
	manifest, err := LoadModelManifest(manifestPath)
	if err != nil {
		return InstalledModel{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return InstalledModel{}, err
	}
	providerRoot := filepath.Join(root, manifest.Provider, manifest.Name)
	if err := os.MkdirAll(providerRoot, 0700); err != nil {
		return InstalledModel{}, err
	}
	destination := filepath.Join(providerRoot, manifest.Version)
	if _, err := os.Lstat(destination); err == nil {
		return InstalledModel{}, fmt.Errorf("model %s@%s is already installed for %s", manifest.Name, manifest.Version, manifest.Provider)
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstalledModel{}, err
	}
	staging, err := os.MkdirTemp(providerRoot, ".install-")
	if err != nil {
		return InstalledModel{}, err
	}
	defer os.RemoveAll(staging)
	sourceRoot := filepath.Dir(manifestPath)
	for _, artifact := range manifest.Artifacts {
		if err := copyModelArtifact(filepath.Join(sourceRoot, artifact.File), filepath.Join(staging, artifact.File), artifact.SHA256); err != nil {
			return InstalledModel{}, fmt.Errorf("install model artifact %q: %w", artifact.File, err)
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return InstalledModel{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "model.json"), append(encoded, '\n'), 0600); err != nil {
		return InstalledModel{}, err
	}
	staged := InstalledModel{Manifest: manifest, Directory: staging}
	if err := inspect(ctx, staged); err != nil {
		return InstalledModel{}, fmt.Errorf("provider rejected model: %w", err)
	}
	storedManifest, err := os.ReadFile(filepath.Join(staging, "model.json"))
	if err != nil {
		return InstalledModel{}, err
	}
	if !bytes.Equal(storedManifest, append(encoded, '\n')) {
		return InstalledModel{}, errors.New("model manifest changed during inspection")
	}
	if err := verifyModelArtifacts(staged); err != nil {
		return InstalledModel{}, fmt.Errorf("model changed during inspection: %w", err)
	}
	if err := os.Rename(staging, destination); err != nil {
		return InstalledModel{}, err
	}
	return InstalledModel{Manifest: manifest, Directory: destination}, nil
}

func copyModelArtifact(source, destination, digest string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxModelArtifactBytes {
		return errors.New("model artifact must be a regular file under 64 GiB")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(output, io.TeeReader(io.LimitReader(input, maxModelArtifactBytes+1), hash))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > maxModelArtifactBytes || hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("model artifact changed or digest mismatched")
	}
	return nil
}

func verifyModelArtifacts(model InstalledModel) error {
	for _, artifact := range model.Manifest.Artifacts {
		path := filepath.Join(model.Directory, artifact.File)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > maxModelArtifactBytes {
			return fmt.Errorf("artifact %q is not a regular model file", artifact.File)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		n, readErr := io.Copy(hash, io.LimitReader(file, maxModelArtifactBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n > maxModelArtifactBytes || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
			return fmt.Errorf("artifact %q digest mismatch", artifact.File)
		}
	}
	return nil
}

func parseModelReference(reference string) (string, string, error) {
	name, version, ok := strings.Cut(reference, "@")
	if !ok || !modelNamePattern.MatchString(name) || !modelVersionPattern.MatchString(version) {
		return "", "", errors.New("model reference must be name@version")
	}
	return name, version, nil
}

// ResolveInstalledModel re-verifies artifact digests before a provider loads
// the model. The provider name comes from the configured provider package.
func ResolveInstalledModel(root, provider, reference string) (InstalledModel, error) {
	if !modelNamePattern.MatchString(provider) {
		return InstalledModel{}, errors.New("model provider name is invalid")
	}
	name, version, err := parseModelReference(reference)
	if err != nil {
		return InstalledModel{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return InstalledModel{}, err
	}
	directory := filepath.Join(root, provider, name, version)
	info, err := os.Lstat(directory)
	if err != nil {
		return InstalledModel{}, err
	}
	if !info.IsDir() {
		return InstalledModel{}, errors.New("installed model directory is invalid")
	}
	manifest, err := LoadModelManifest(filepath.Join(directory, "model.json"))
	if err != nil {
		return InstalledModel{}, err
	}
	if manifest.Provider != provider || manifest.Name != name || manifest.Version != version {
		return InstalledModel{}, errors.New("installed model manifest does not match its directory")
	}
	model := InstalledModel{Manifest: manifest, Directory: directory}
	if err := verifyModelArtifacts(model); err != nil {
		return InstalledModel{}, err
	}
	return model, nil
}

func ListInstalledModels(root string) ([]InstalledModel, error) {
	providers, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []InstalledModel{}, nil
	}
	if err != nil {
		return nil, err
	}
	models := make([]InstalledModel, 0)
	for _, provider := range providers {
		if !provider.IsDir() || !modelNamePattern.MatchString(provider.Name()) {
			continue
		}
		names, err := os.ReadDir(filepath.Join(root, provider.Name()))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if !name.IsDir() || !modelNamePattern.MatchString(name.Name()) {
				continue
			}
			versions, err := os.ReadDir(filepath.Join(root, provider.Name(), name.Name()))
			if err != nil {
				return nil, err
			}
			for _, version := range versions {
				if !version.IsDir() || !modelVersionPattern.MatchString(version.Name()) {
					continue
				}
				model, err := ResolveInstalledModel(root, provider.Name(), name.Name()+"@"+version.Name())
				if err != nil {
					return nil, fmt.Errorf("installed model %s@%s: %w", name.Name(), version.Name(), err)
				}
				models = append(models, model)
			}
		}
	}
	slices.SortFunc(models, func(a, b InstalledModel) int {
		return strings.Compare(a.Manifest.Provider+"/"+a.Manifest.Name+"@"+a.Manifest.Version, b.Manifest.Provider+"/"+b.Manifest.Name+"@"+b.Manifest.Version)
	})
	return models, nil
}

// RemoveInstalledModel deletes one verified installed version and prunes its
// empty model and provider directories.
func RemoveInstalledModel(root, provider, reference string) error {
	model, err := ResolveInstalledModel(root, provider, reference)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(model.Directory); err != nil {
		return err
	}
	modelRoot := filepath.Dir(model.Directory)
	providerRoot := filepath.Dir(modelRoot)
	_ = os.Remove(modelRoot)
	_ = os.Remove(providerRoot)
	return nil
}
