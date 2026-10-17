package contentengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"jangolova/contentkit"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func LoadModelManifest(path string) (contentkit.ModelManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return contentkit.ModelManifest{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxModelManifestBytes+1))
	if err != nil {
		return contentkit.ModelManifest{}, err
	}
	if len(data) > maxModelManifestBytes {
		return contentkit.ModelManifest{}, errors.New("model manifest exceeds 64 KiB")
	}
	var manifest contentkit.ModelManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return contentkit.ModelManifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return contentkit.ModelManifest{}, errors.New("model manifest must contain one JSON object")
	}
	// Manifests installed before the neutral names keep loading for one release.
	if manifest.APIVersion == "blockade.model/v1alpha1" || manifest.APIVersion == "cymonkey.model/v1alpha1" {
		manifest.APIVersion = contentkit.ModelManifestAPIVersion
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
func InstallModel(ctx context.Context, root, manifestPath string, inspect func(context.Context, contentkit.InstalledModel) error) (contentkit.InstalledModel, error) {
	if inspect == nil {
		return contentkit.InstalledModel{}, errors.New("model installation requires provider inspection")
	}
	manifest, err := LoadModelManifest(manifestPath)
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	providerRoot := filepath.Join(root, manifest.Provider, manifest.Name)
	if err := os.MkdirAll(providerRoot, 0700); err != nil {
		return contentkit.InstalledModel{}, err
	}
	destination := filepath.Join(providerRoot, manifest.Version)
	if _, err := os.Lstat(destination); err == nil {
		return contentkit.InstalledModel{}, fmt.Errorf("model %s@%s is already installed for %s", manifest.Name, manifest.Version, manifest.Provider)
	} else if !errors.Is(err, os.ErrNotExist) {
		return contentkit.InstalledModel{}, err
	}
	staging, err := os.MkdirTemp(providerRoot, ".install-")
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	defer os.RemoveAll(staging)
	sourceRoot := filepath.Dir(manifestPath)
	for _, artifact := range manifest.Artifacts {
		if err := copyModelArtifact(filepath.Join(sourceRoot, artifact.File), filepath.Join(staging, artifact.File), artifact.SHA256); err != nil {
			return contentkit.InstalledModel{}, fmt.Errorf("install model artifact %q: %w", artifact.File, err)
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "model.json"), append(encoded, '\n'), 0600); err != nil {
		return contentkit.InstalledModel{}, err
	}
	staged := contentkit.InstalledModel{Manifest: manifest, Directory: staging}
	if err := inspect(ctx, staged); err != nil {
		return contentkit.InstalledModel{}, fmt.Errorf("provider rejected model: %w", err)
	}
	storedManifest, err := os.ReadFile(filepath.Join(staging, "model.json"))
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	if !bytes.Equal(storedManifest, append(encoded, '\n')) {
		return contentkit.InstalledModel{}, errors.New("model manifest changed during inspection")
	}
	if err := verifyModelArtifacts(staged); err != nil {
		return contentkit.InstalledModel{}, fmt.Errorf("model changed during inspection: %w", err)
	}
	if err := os.Rename(staging, destination); err != nil {
		return contentkit.InstalledModel{}, err
	}
	return contentkit.InstalledModel{Manifest: manifest, Directory: destination}, nil
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

func verifyModelArtifacts(model contentkit.InstalledModel) error {
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
	if !ok || !contentkit.ModelNamePattern.MatchString(name) || !contentkit.ModelVersionPattern.MatchString(version) {
		return "", "", errors.New("model reference must be name@version")
	}
	return name, version, nil
}

// ResolveInstalledModel re-verifies artifact digests before a provider loads
// the model. The provider name comes from the configured provider package.
func ResolveInstalledModel(root, provider, reference string) (contentkit.InstalledModel, error) {
	if !contentkit.ModelNamePattern.MatchString(provider) {
		return contentkit.InstalledModel{}, errors.New("model provider name is invalid")
	}
	name, version, err := parseModelReference(reference)
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	directory := filepath.Join(root, provider, name, version)
	info, err := os.Lstat(directory)
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	if !info.IsDir() {
		return contentkit.InstalledModel{}, errors.New("installed model directory is invalid")
	}
	manifest, err := LoadModelManifest(filepath.Join(directory, "model.json"))
	if err != nil {
		return contentkit.InstalledModel{}, err
	}
	if manifest.Provider != provider || manifest.Name != name || manifest.Version != version {
		return contentkit.InstalledModel{}, errors.New("installed model manifest does not match its directory")
	}
	model := contentkit.InstalledModel{Manifest: manifest, Directory: directory}
	if err := verifyModelArtifacts(model); err != nil {
		return contentkit.InstalledModel{}, err
	}
	return model, nil
}

func ListInstalledModels(root string) ([]contentkit.InstalledModel, error) {
	providers, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []contentkit.InstalledModel{}, nil
	}
	if err != nil {
		return nil, err
	}
	models := make([]contentkit.InstalledModel, 0)
	for _, provider := range providers {
		if !provider.IsDir() || !contentkit.ModelNamePattern.MatchString(provider.Name()) {
			continue
		}
		names, err := os.ReadDir(filepath.Join(root, provider.Name()))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if !name.IsDir() || !contentkit.ModelNamePattern.MatchString(name.Name()) {
				continue
			}
			versions, err := os.ReadDir(filepath.Join(root, provider.Name(), name.Name()))
			if err != nil {
				return nil, err
			}
			for _, version := range versions {
				if !version.IsDir() || !contentkit.ModelVersionPattern.MatchString(version.Name()) {
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
	slices.SortFunc(models, func(a, b contentkit.InstalledModel) int {
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

const maxModelManifestBytes = 64 << 10
const maxModelArtifactBytes int64 = 64 << 30
