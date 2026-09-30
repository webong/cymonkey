package blockadecli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"blockade"
)

// Run executes Blockade without pre-registered providers. Applications supply
// installed providers through RunWithProviderAdapters.
func Run(args []string, stdout, stderr io.Writer) error {
	registry := blockade.NewProviderAdapterRegistry()
	return RunWithProviderAdapters(args, stdout, stderr, registry, blockade.EnvironmentSecretResolver{})
}

func RunWithProviderAdapters(args []string, stdout, stderr io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver) error {
	if len(args) == 0 {
		return errors.New("blockade requires validate, model, runtime, observe, interpret, or serve")
	}
	switch args[0] {
	case "validate":
		return validateCommand(args[1:], stdout)
	case "model":
		return modelCommand(args[1:], stdout, registry, resolver)
	case "runtime":
		return runtimeCommand(args[1:], stdout)
	case "observe":
		return observeCommand(args[1:], stdout, registry, resolver)
	case "interpret":
		return interpretCommand(args[1:], stdout, registry, resolver)
	case "serve":
		return serveCommand(args[1:], stderr, registry, resolver)
	default:
		return fmt.Errorf("unknown blockade command %q", args[0])
	}
}

func runtimeCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("blockade runtime requires manifest, install, list, or remove")
	}
	switch args[0] {
	case "manifest":
		flags := flag.NewFlagSet("blockade runtime manifest", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		source := flags.String("source", "", "self-contained runtime directory")
		name := flags.String("name", "", "runtime name")
		version := flags.String("version", "", "runtime semantic version")
		executable := flags.String("executable", "", "runtime-relative executable path")
		output := flags.String("output", "", "output runtime.json path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*source) == "" || strings.TrimSpace(*name) == "" || strings.TrimSpace(*version) == "" || strings.TrimSpace(*executable) == "" || strings.TrimSpace(*output) == "" {
			return errors.New("blockade runtime manifest requires --source, --name, --version, --executable, and --output")
		}
		manifest, err := blockade.BuildRuntimeManifest(*source, strings.TrimSpace(*name), strings.TrimSpace(*version), strings.TrimSpace(*executable))
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*output, append(encoded, '\n'), 0600); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, *output)
		return err
	case "install":
		flags := flag.NewFlagSet("blockade runtime install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifestPath := flags.String("manifest", "", "local runtime.json path")
		runtimeRoot := flags.String("root", "", "installed runtime directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifestPath) == "" {
			return errors.New("blockade runtime install requires --manifest")
		}
		root := strings.TrimSpace(*runtimeRoot)
		var err error
		if root == "" {
			root, err = blockade.DefaultRuntimeRoot()
			if err != nil {
				return err
			}
		}
		installed, err := blockade.InstallRuntime(root, *manifestPath)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(installed)
	case "list":
		flags := flag.NewFlagSet("blockade runtime list", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		runtimeRoot := flags.String("root", "", "installed runtime directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("blockade runtime list accepts flags only")
		}
		root := strings.TrimSpace(*runtimeRoot)
		var err error
		if root == "" {
			root, err = blockade.DefaultRuntimeRoot()
			if err != nil {
				return err
			}
		}
		runtimes, err := blockade.ListInstalledRuntimes(root)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"runtimes": runtimes})
	case "remove":
		flags := flag.NewFlagSet("blockade runtime remove", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		name := flags.String("name", "", "installed runtime name")
		version := flags.String("version", "", "installed runtime version")
		runtimeRoot := flags.String("root", "", "installed runtime directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*name) == "" || strings.TrimSpace(*version) == "" {
			return errors.New("blockade runtime remove requires --name and --version")
		}
		root := strings.TrimSpace(*runtimeRoot)
		var err error
		if root == "" {
			root, err = blockade.DefaultRuntimeRoot()
			if err != nil {
				return err
			}
		}
		if err := blockade.RemoveInstalledRuntime(root, strings.TrimSpace(*name), strings.TrimSpace(*version)); err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "removed runtime %s@%s\n", strings.TrimSpace(*name), strings.TrimSpace(*version))
		return err
	default:
		return fmt.Errorf("unknown blockade runtime command %q", args[0])
	}
}

func modelCommand(args []string, stdout io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver) error {
	if len(args) == 0 {
		return errors.New("blockade model requires install, list, or remove")
	}
	switch args[0] {
	case "install":
		flags := flag.NewFlagSet("blockade model install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifestPath := flags.String("manifest", "", "local model.json path")
		configPath := flags.String("config", "", "Blockade YAML config containing the provider")
		providerID := flags.String("provider", "", "provider adapter id when config has more than one matching provider")
		modelRoot := flags.String("root", "", "installed model directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifestPath) == "" || strings.TrimSpace(*configPath) == "" {
			return errors.New("blockade model install requires --manifest and --config")
		}
		manifest, err := blockade.LoadModelManifest(*manifestPath)
		if err != nil {
			return err
		}
		config, err := blockade.LoadConfig(*configPath)
		if err != nil {
			return err
		}
		provider, err := selectModelProvider(config, manifest.Provider, *providerID)
		if err != nil {
			return err
		}
		root := strings.TrimSpace(*modelRoot)
		if root == "" {
			root, err = blockade.DefaultModelRoot()
			if err != nil {
				return err
			}
		}
		installed, err := blockade.InstallModel(context.Background(), root, *manifestPath, func(ctx context.Context, model blockade.InstalledModel) error {
			return blockade.InspectConfiguredProviderModel(ctx, provider, model, registry, resolver)
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(installed)
	case "list":
		flags := flag.NewFlagSet("blockade model list", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		modelRoot := flags.String("root", "", "installed model directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("blockade model list accepts flags only")
		}
		root := strings.TrimSpace(*modelRoot)
		var err error
		if root == "" {
			root, err = blockade.DefaultModelRoot()
			if err != nil {
				return err
			}
		}
		models, err := blockade.ListInstalledModels(root)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"models": models})
	case "remove":
		flags := flag.NewFlagSet("blockade model remove", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		provider := flags.String("provider", "", "installed provider name")
		model := flags.String("model", "", "installed model reference name@version")
		modelRoot := flags.String("root", "", "installed model directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*provider) == "" || strings.TrimSpace(*model) == "" {
			return errors.New("blockade model remove requires --provider and --model")
		}
		root := strings.TrimSpace(*modelRoot)
		var err error
		if root == "" {
			root, err = blockade.DefaultModelRoot()
			if err != nil {
				return err
			}
		}
		if err := blockade.RemoveInstalledModel(root, strings.TrimSpace(*provider), strings.TrimSpace(*model)); err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "removed model %s/%s\n", strings.TrimSpace(*provider), strings.TrimSpace(*model))
		return err
	default:
		return fmt.Errorf("unknown blockade model command %q", args[0])
	}
}

func selectModelProvider(config blockade.Config, providerName, id string) (blockade.ProviderAdapterConfig, error) {
	id = strings.TrimSpace(id)
	var matches []blockade.ProviderAdapterConfig
	for _, provider := range config.Providers {
		if id != "" && provider.ID != id {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(provider.Provider), strings.TrimSpace(providerName)) {
			matches = append(matches, provider)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if id != "" {
		return blockade.ProviderAdapterConfig{}, fmt.Errorf("provider %q does not use model provider %q", id, providerName)
	}
	if len(matches) == 0 {
		return blockade.ProviderAdapterConfig{}, fmt.Errorf("config has no provider package named %q", providerName)
	}
	return blockade.ProviderAdapterConfig{}, fmt.Errorf("config has multiple providers using %q; select one with --provider", providerName)
}

func interpretCommand(args []string, stdout io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver) error {
	flags := flag.NewFlagSet("blockade interpret", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	inferenceID := flags.String("inference", "", "model adapter id")
	text := flags.String("text", "", "text content")
	textFile := flags.String("text-file", "", "UTF-8 text file")
	imagePath := flags.String("image", "", "image file")
	audioPath := flags.String("audio", "", "audio file")
	audioType := flags.String("audio-type", "", "audio MIME type, such as audio/wav")
	videoPath := flags.String("video", "", "video file")
	videoType := flags.String("video-type", "", "video MIME type, such as video/mp4")
	prompt := flags.String("prompt", "", "optional interpretation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*configPath) == "" {
		return errors.New("blockade interpret requires --config and content flags")
	}
	parts := make([]blockade.ContentPart, 0, 4)
	if *text != "" {
		parts = append(parts, blockade.ContentPart{Kind: "text", Text: *text})
	}
	if *textFile != "" {
		data, err := readInterpretFile(*textFile)
		if err != nil {
			return err
		}
		parts = append(parts, blockade.ContentPart{Kind: "text", Text: string(data)})
	}
	if *imagePath != "" {
		data, err := readInterpretFile(*imagePath)
		if err != nil {
			return err
		}
		parts = append(parts, blockade.ContentPart{Kind: "image", MediaType: http.DetectContentType(data), Data: data})
	}
	if *audioPath != "" {
		if *audioType == "" {
			return errors.New("--audio-type is required with --audio")
		}
		data, err := readInterpretFile(*audioPath)
		if err != nil {
			return err
		}
		parts = append(parts, blockade.ContentPart{Kind: "audio", MediaType: *audioType, Data: data})
	}
	if *videoPath != "" {
		if *videoType == "" {
			return errors.New("--video-type is required with --video")
		}
		data, err := readInterpretFile(*videoPath)
		if err != nil {
			return err
		}
		parts = append(parts, blockade.ContentPart{Kind: "video", MediaType: *videoType, Data: data})
	}
	request := blockade.InterpretRequest{APIVersion: blockade.ContentAPIVersion, Parts: parts, Prompt: *prompt}
	if err := blockade.ValidateInterpretRequest(request); err != nil {
		return err
	}
	config, err := blockade.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	selected, err := selectProvider(config, *inferenceID)
	if err != nil {
		return err
	}
	started, err := blockade.StartConfiguredInference(context.Background(), selected, registry, resolver)
	if err != nil {
		return err
	}
	defer started.Close()
	result, err := (blockade.Client{Engine: started}).Interpret(context.Background(), request)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func readInterpretFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 10<<20 {
		return nil, errors.New("content file exceeds 10 MiB")
	}
	return data, nil
}

func validateCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("blockade validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	checkFiles := flags.Bool("check-files", false, "verify local model files exist")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("blockade validate accepts flags only")
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}
	config, err := blockade.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if *checkFiles {
		if err := config.ValidateModelFiles(); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "valid Blockade config: %d provider(s)\n", len(config.Providers))
	return err
}

func observeCommand(args []string, stdout io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver) error {
	flags := flag.NewFlagSet("blockade observe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	inferenceID := flags.String("inference", "", "provider instance id")
	imagePath := flags.String("image", "", "PNG or JPEG image path")
	prompt := flags.String("prompt", "", "optional observation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("blockade observe accepts flags only")
	}
	if strings.TrimSpace(*configPath) == "" || strings.TrimSpace(*imagePath) == "" {
		return errors.New("--config and --image are required")
	}
	config, err := blockade.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	selected, err := selectProvider(config, *inferenceID)
	if err != nil {
		return err
	}
	started, err := blockade.StartConfiguredInference(context.Background(), selected, registry, resolver)
	if err != nil {
		return err
	}
	defer started.Close()
	image, err := os.ReadFile(*imagePath)
	if err != nil {
		return err
	}
	result, err := (blockade.Client{Engine: started}).Observe(context.Background(), blockade.ObserveRequest{Image: image, Prompt: *prompt})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func serveCommand(args []string, stderr io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver) error {
	flags := flag.NewFlagSet("blockade serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	inferenceID := flags.String("inference", "", "provider instance id")
	bind := flags.String("bind", envOrDefault("BLOCKADE_BIND", "127.0.0.1:8091"), "Blockade HTTP bind address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("blockade serve accepts flags only")
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}
	config, err := blockade.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	selected, err := selectProvider(config, *inferenceID)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	engine, err := blockade.StartConfiguredInference(ctx, selected, registry, resolver)
	if err != nil {
		return err
	}
	defer engine.Close()
	service, err := blockade.NewHTTPServer(engine, selected.ID)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *bind, Handler: service.Routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	_, _ = fmt.Fprintf(stderr, "blockade service listening on %s\n", *bind)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve Blockade: %w", err)
	}
	return nil
}

func selectProvider(config blockade.Config, inferenceID string) (blockade.ProviderAdapterConfig, error) {
	inferenceID = strings.TrimSpace(inferenceID)
	if inferenceID == "" {
		selected, ok := config.DefaultProvider()
		if !ok {
			return blockade.ProviderAdapterConfig{}, errors.New("Blockade config has no provider")
		}
		return selected, nil
	}
	selected, ok := config.Provider(inferenceID)
	if !ok {
		return blockade.ProviderAdapterConfig{}, fmt.Errorf("Blockade provider %q is not configured", inferenceID)
	}
	return selected, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
