package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func defaultContentModelRoot() (string, error) {
	return contentStorageRoot("CYMONKEY_MODEL_DIR", "models")
}

func defaultContentRuntimeRoot() (string, error) {
	return contentStorageRoot("CYMONKEY_RUNTIME_DIR", "runtimes")
}

func contentStorageRoot(environment, name string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(environment)); value != "" {
		return filepath.Abs(value)
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "cymonkey", "content", name), nil
}

// prepareContentArtifacts converts Cymonkey model and runtime manifests into
// temporary library manifests. A generated runtime manifest is converted back
// only after the library has completed and verified its inventory.
func prepareContentArtifacts(args []string) ([]string, func(), func() error, error) {
	result := append([]string(nil), args...)
	cleanupPaths := []string{}
	cleanup := func() {
		for _, path := range cleanupPaths {
			_ = os.Remove(path)
		}
	}
	commit := func() error { return nil }

	if len(args) >= 2 && args[0] == "model" {
		root, err := defaultContentModelRoot()
		if err != nil {
			return nil, cleanup, commit, err
		}
		if _, ok := contentFlagValue(args, "root"); !ok {
			result = append(result, "--root", root)
		}
		if args[1] == "install" {
			result, cleanupPaths, err = translateContentManifestInput(result, cleanupPaths, "manifest")
			if err != nil {
				cleanup()
				return nil, cleanup, commit, err
			}
		}
	}

	if len(args) >= 2 && args[0] == "runtime" {
		if args[1] == "manifest" {
			publicOutput, ok := contentFlagValue(args, "output")
			if !ok || strings.TrimSpace(publicOutput) == "" {
				return result, cleanup, commit, nil
			}
			temporary, err := temporaryContentFile("runtime-manifest-*.json", false)
			if err != nil {
				return nil, cleanup, commit, err
			}
			cleanupPaths = append(cleanupPaths, temporary)
			result = replaceContentFlag(result, "output", temporary)
			commit = func() error {
				value, err := readJSONObject(temporary)
				if err != nil {
					return err
				}
				return writePublicJSON(publicOutput, publicContentJSON(value))
			}
			return result, cleanup, commit, nil
		}
		root, err := defaultContentRuntimeRoot()
		if err != nil {
			return nil, cleanup, commit, err
		}
		if _, ok := contentFlagValue(args, "root"); !ok {
			result = append(result, "--root", root)
		}
		if args[1] == "install" {
			result, cleanupPaths, err = translateContentManifestInput(result, cleanupPaths, "manifest")
			if err != nil {
				cleanup()
				return nil, cleanup, commit, err
			}
		}
	}
	return result, cleanup, commit, nil
}

func translateContentManifestInput(args []string, cleanupPaths []string, flagName string) ([]string, []string, error) {
	path, ok := contentFlagValue(args, flagName)
	if !ok || strings.TrimSpace(path) == "" {
		return args, cleanupPaths, nil
	}
	value, err := readJSONObject(path)
	if err != nil {
		return nil, cleanupPaths, err
	}
	temporaryFile, err := os.CreateTemp(filepath.Dir(path), ".cymonkey-manifest-*.json")
	if err != nil {
		return nil, cleanupPaths, err
	}
	temporary := temporaryFile.Name()
	if err := temporaryFile.Close(); err != nil {
		_ = os.Remove(temporary)
		return nil, cleanupPaths, err
	}
	cleanupPaths = append(cleanupPaths, temporary)
	if err := writePublicJSON(temporary, libraryContentJSON(value)); err != nil {
		return nil, cleanupPaths, err
	}
	return replaceContentFlag(args, flagName, temporary), cleanupPaths, nil
}

func temporaryContentFile(pattern string, keep bool) (string, error) {
	file, err := os.CreateTemp("", "cymonkey-"+pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if !keep {
		if err := os.Remove(path); err != nil {
			return "", err
		}
	}
	return path, nil
}

func readJSONObject(path string) (any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, (16<<20)+1))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, errors.New("manifest must contain one JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("manifest must contain one JSON object")
	}
	return value, nil
}

func writePublicJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	return os.WriteFile(abs, append(encoded, '\n'), 0o600)
}

func libraryContentJSON(value any) any {
	switch value := value.(type) {
	case []any:
		for index := range value {
			value[index] = libraryContentJSON(value[index])
		}
		return value
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			if key == "plugin" {
				key = "provider"
			}
			result[key] = libraryContentJSON(item)
		}
		return result
	}
	return value
}

func writeRuntimeManifestResult(args []string, stdout io.Writer) (bool, error) {
	if len(args) < 2 || args[0] != "runtime" || args[1] != "manifest" {
		return false, nil
	}
	path, ok := contentFlagValue(args, "output")
	if !ok {
		return false, nil
	}
	_, err := fmt.Fprintln(stdout, path)
	return true, err
}
