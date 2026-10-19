package contentcmd

import (
	"bytes"
	contentcli "console/contentcli"
	"console/contentengine"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"jangolova/contentkit"
	"jangolova/contentkit/public"
	"os"
	"path/filepath"
	"strings"
)

// Run adapts Cymonkey's public content command and data contracts to
// the internal interpretation library. No library contract is exposed here.
func Run(args []string, stdout, stderr io.Writer) error {
	return RunContext(context.Background(), args, stdout, stderr)
}

// RunContext is Run with a context that ends a long-running command such as
// serve when it is canceled.
func RunContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "service" {
		return runContentService(args[1:], stdout)
	}
	registry, err := contentPluginRegistry()
	if err != nil {
		return publicContentError(err)
	}
	defaultConfig, err := defaultContentConfigPath()
	if err != nil {
		return err
	}

	publicArgs := append([]string(nil), args...)
	if err := rejectInternalContentVocabulary(publicArgs); err != nil {
		return err
	}
	publicConfig := contentConfigPath(publicArgs, defaultConfig)
	if isContentEndpointList(publicArgs) {
		return listContentEndpoints(publicConfig, stdout)
	}
	if isContentValidate(publicArgs) {
		return validateContentConfig(publicArgs, publicConfig, stdout)
	}

	libraryArgs := translateContentArgs(publicArgs)
	libraryArgs, artifactCleanup, commitArtifacts, err := prepareContentArtifacts(libraryArgs)
	if err != nil {
		return publicContentError(err)
	}
	defer artifactCleanup()
	cleanup := func() {}
	if contentCommandUsesConfig(publicArgs) {
		var temporaryConfig string
		libraryArgs, temporaryConfig, cleanup, err = prepareLibraryContentConfig(libraryArgs, publicConfig)
		if err != nil {
			return publicContentError(err)
		}
		defer cleanup()
		defaultConfig = temporaryConfig
	}

	engine := contentcli.CommandEngine{
		Invocation:    "cymy content",
		DefaultConfig: defaultConfig,
		DefaultBind:   envOrDefault("CYMONKEY_CONTENT_BIND", "127.0.0.1:8091"),
		Registry:      registry,
		Resolver:      contentengine.EnvironmentSecretResolver{},
		Middleware:    contentServiceContract,
		Context:       ctx,
	}
	if len(publicArgs) > 0 && publicArgs[0] == "serve" {
		return publicContentError(engine.Run(libraryArgs, stdout, contentLogWriter{writer: stderr}))
	}

	var libraryOutput bytes.Buffer
	if err := engine.Run(libraryArgs, &libraryOutput, contentLogWriter{writer: stderr}); err != nil {
		return publicContentError(err)
	}
	if err := commitArtifacts(); err != nil {
		return publicContentError(err)
	}
	if isContentEndpointMutation(publicArgs) {
		if err := persistContentEndpointMutation(publicArgs, publicConfig, defaultConfig); err != nil {
			return publicContentError(err)
		}
		return writeContentEndpointMutationResult(publicArgs, publicConfig, stdout)
	}
	if written, err := writeRuntimeManifestResult(publicArgs, stdout); written || err != nil {
		return err
	}
	return writePublicContentOutput(publicArgs, publicConfig, libraryOutput.Bytes(), stdout)
}

func defaultContentConfigPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("CYMONKEY_CONTENT_CONFIG")); value != "" {
		return filepath.Abs(value)
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "cymonkey", "content.yaml"), nil
}

func contentConfigPath(args []string, fallback string) string {
	if value, ok := contentFlagValue(args, "config"); ok && strings.TrimSpace(value) != "" {
		if absolute, err := filepath.Abs(value); err == nil {
			return absolute
		}
		return value
	}
	return fallback
}

func contentFlagValue(args []string, name string) (string, bool) {
	prefix := "--" + name + "="
	for index, argument := range args {
		if strings.HasPrefix(argument, prefix) {
			return strings.TrimPrefix(argument, prefix), true
		}
		if argument == "--"+name && index+1 < len(args) {
			return args[index+1], true
		}
	}
	return "", false
}

func replaceContentFlag(args []string, name, value string) []string {
	result := append([]string(nil), args...)
	prefix := "--" + name + "="
	for index, argument := range result {
		if strings.HasPrefix(argument, prefix) {
			result[index] = prefix + value
			return result
		}
		if argument == "--"+name && index+1 < len(result) {
			result[index+1] = value
			return result
		}
	}
	return append(result, "--"+name, value)
}

func translateContentArgs(args []string) []string {
	result := append([]string(nil), args...)
	var replacements map[string]string
	if len(args) >= 2 && args[0] == "endpoint" && args[1] == "add" {
		replacements = map[string]string{"plugin": "provider"}
	}
	if len(args) >= 2 && args[0] == "model" && args[1] == "install" {
		replacements = map[string]string{"adapter": "provider"}
	}
	if len(args) >= 2 && args[0] == "model" && args[1] == "remove" {
		replacements = map[string]string{"plugin": "provider"}
	}
	for from, to := range replacements {
		for index, argument := range result {
			if argument == "--"+from {
				result[index] = "--" + to
			}
			if strings.HasPrefix(argument, "--"+from+"=") {
				result[index] = "--" + to + "=" + strings.TrimPrefix(argument, "--"+from+"=")
			}
		}
	}
	return result
}

func rejectInternalContentVocabulary(args []string) error {
	for _, argument := range args {
		if argument == "--provider" || strings.HasPrefix(argument, "--provider=") {
			return errors.New("use --plugin for a plugin name or --adapter for a configured adapter")
		}
	}
	return nil
}

func contentCommandUsesConfig(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "interpret", "observe", "serve":
		return true
	case "endpoint":
		return len(args) > 1 && args[1] != "list" && args[1] != "help" && args[1] != "-h" && args[1] != "--help"
	case "model":
		return len(args) > 1 && args[1] == "install"
	default:
		return false
	}
}

func prepareLibraryContentConfig(args []string, publicPath string) ([]string, string, func(), error) {
	temporary, err := os.CreateTemp("", "cymonkey-content-*.yaml")
	if err != nil {
		return nil, "", nil, err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return nil, "", nil, err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return nil, "", nil, err
	}
	cleanup := func() { _ = os.Remove(temporaryPath) }
	config, err := LoadConfig(publicPath)
	if err == nil {
		libraryConfig := ToLibraryConfig(config)
		modelRoot, rootErr := defaultContentModelRoot()
		if rootErr != nil {
			cleanup()
			return nil, "", nil, rootErr
		}
		for index := range libraryConfig.Providers {
			if libraryConfig.Providers[index].Model != "" && libraryConfig.Providers[index].ModelRoot == "" {
				libraryConfig.Providers[index].ModelRoot = modelRoot
			}
		}
		if err := contentengine.SaveConfig(temporaryPath, libraryConfig); err != nil {
			cleanup()
			return nil, "", nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		cleanup()
		return nil, "", nil, err
	}
	return replaceContentFlag(args, "config", temporaryPath), temporaryPath, cleanup, nil
}

func isContentEndpointList(args []string) bool {
	return len(args) >= 2 && args[0] == "endpoint" && args[1] == "list"
}

func isContentEndpointMutation(args []string) bool {
	return len(args) >= 2 && args[0] == "endpoint" && (args[1] == "add" || args[1] == "remove")
}

func isContentValidate(args []string) bool {
	return len(args) > 0 && args[0] == "validate"
}

func listContentEndpoints(path string, stdout io.Writer) error {
	config, err := LoadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return json.NewEncoder(stdout).Encode(map[string]any{"apiVersion": public.ContentConfigVersion, "config": path, "endpoints": []public.ContentAdapterConfig{}})
	}
	if err != nil {
		return publicContentError(err)
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"apiVersion": public.ContentConfigVersion, "config": path, "endpoints": config.Adapters})
}

func validateContentConfig(args []string, path string, stdout io.Writer) error {
	flags := flag.NewFlagSet("cymy content validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", path, "content YAML config")
	checkFiles := flags.Bool("check-files", false, "verify local model files exist")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cymy content validate accepts flags only")
	}
	config, err := LoadConfig(*configPath)
	if err != nil {
		return publicContentError(err)
	}
	if *checkFiles {
		if err := ToLibraryConfig(config).ValidateModelFiles(); err != nil {
			return publicContentError(err)
		}
	}
	_, err = fmt.Fprintf(stdout, "valid content config: %d adapter(s)\n", len(config.Adapters))
	return err
}

func persistContentEndpointMutation(args []string, publicPath, temporaryPath string) error {
	config, err := contentengine.LoadConfig(temporaryPath)
	if errors.Is(err, os.ErrNotExist) && len(args) >= 2 && args[1] == "remove" {
		if removeErr := os.Remove(publicPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	return SaveConfig(publicPath, FromLibraryConfig(config))
}

func writeContentEndpointMutationResult(args []string, configPath string, stdout io.Writer) error {
	id, _ := contentFlagValue(args, "id")
	if args[1] == "remove" {
		_, err := fmt.Fprintf(stdout, "removed content endpoint %s\n", strings.TrimSpace(id))
		return err
	}
	config, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	for _, adapter := range config.Adapters {
		if adapter.ID == strings.TrimSpace(id) {
			return json.NewEncoder(stdout).Encode(map[string]any{"apiVersion": public.ContentConfigVersion, "config": configPath, "endpoint": adapter})
		}
	}
	return fmt.Errorf("content endpoint %q was not saved", strings.TrimSpace(id))
}

func writePublicContentOutput(args []string, configPath string, output []byte, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "interpret" {
		var response contentkit.InterpretResponse
		if err := json.Unmarshal(output, &response); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(public.FromLibraryResponse(response))
	}
	if len(args) > 0 && args[0] == "observe" {
		var response contentkit.ObserveResponse
		if err := json.Unmarshal(output, &response); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(public.FromLibraryObservation(response))
	}
	if len(bytes.TrimSpace(output)) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(output, &value) == nil {
		value = publicContentJSON(value)
		if object, ok := value.(map[string]any); ok && len(args) >= 2 && args[0] == "endpoint" && args[1] == "test" {
			object["config"] = configPath
		}
		return json.NewEncoder(stdout).Encode(value)
	}
	_, err := stdout.Write([]byte(publicContentText(string(output))))
	return err
}

func publicContentJSON(value any) any {
	switch value := value.(type) {
	case []any:
		for index := range value {
			value[index] = publicContentJSON(value[index])
		}
		return value
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			publicKey := key
			switch key {
			case "provider":
				publicKey = "plugin"
			case "providers":
				publicKey = "adapters"
			}
			result[publicKey] = publicContentJSON(item)
		}
		return result
	case string:
		if value == contentengine.ConfigAPIVersion {
			return public.ContentConfigVersion
		}
		return value
	default:
		return value
	}
}

func publicContentError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(publicContentText(err.Error()))
}

func publicContentText(value string) string {
	return strings.NewReplacer(
		"Blockade provider adapter", "content adapter",
		"Blockade provider", "content adapter",
		"provider packages", "plugins",
		"provider package", "plugin",
		"--provider", "--plugin",
		"provider adapter", "adapter",
		"provider instance", "adapter",
		"provider process", "adapter process",
		"providers", "adapters",
		"provider", "adapter",
		"Blockade config", "content config",
		"Blockade service", "content service",
		"Blockade bearer token", "content bearer token",
		"Blockade", "content",
		"blockade", "content",
	).Replace(value)
}

type contentLogWriter struct{ writer io.Writer }

func (writer contentLogWriter) Write(data []byte) (int, error) {
	translated := []byte(publicContentText(string(data)))
	if _, err := writer.writer.Write(translated); err != nil {
		return 0, err
	}
	return len(data), nil
}
