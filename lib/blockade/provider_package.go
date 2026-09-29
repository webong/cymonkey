package blockade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"strings"
)

const ProviderPackageAPIVersion = "blockade.provider-package/v1alpha1"

// ProviderPackage describes how Blockade runs a source-only provider package.
// It is deliberately separate from plugin.json: the shared package installer
// verifies bytes, while Blockade owns and interprets this runtime contract.
type ProviderPackage struct {
	APIVersion string                 `json:"apiVersion"`
	Runtime    ProviderPackageRuntime `json:"runtime"`
	Entrypoint string                 `json:"entrypoint"`
}

type ProviderPackageRuntime struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (p ProviderPackage) Validate() error {
	if p.APIVersion != ProviderPackageAPIVersion {
		return fmt.Errorf("provider package apiVersion must be %q", ProviderPackageAPIVersion)
	}
	if p.Runtime.Name != "python" || !modelVersionPattern.MatchString(p.Runtime.Version) {
		return errors.New("provider package requires an exact managed Python runtime version")
	}
	if !validProviderPackagePath(p.Entrypoint) || !strings.HasSuffix(strings.ToLower(p.Entrypoint), ".py") {
		return errors.New("provider package entrypoint must be a relative Python file")
	}
	return nil
}

func validProviderPackagePath(value string) bool {
	return value != "" && value != "." && value != ".." && value != "blockade.json" &&
		!strings.Contains(value, `\`) && !strings.HasPrefix(value, "/") &&
		pathpkg.Clean(value) == value && !strings.HasPrefix(value, "../")
}

func LoadProviderPackage(path string) (ProviderPackage, error) {
	file, err := os.Open(path)
	if err != nil {
		return ProviderPackage{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil {
		return ProviderPackage{}, err
	}
	if len(data) > 64<<10 {
		return ProviderPackage{}, errors.New("provider package descriptor exceeds 64 KiB")
	}
	var descriptor ProviderPackage
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&descriptor); err != nil {
		return ProviderPackage{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ProviderPackage{}, errors.New("provider package descriptor must contain one JSON object")
	}
	return descriptor, descriptor.Validate()
}
