package contentcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"blockade"
)

// CommandEngine owns Cymonkey's content commands and delegates inference,
// validation, model storage, and runtime storage to the content library.
type CommandEngine struct {
	Invocation    string
	DefaultConfig string
	DefaultBind   string
	Registry      *blockade.ProviderAdapterRegistry
	Resolver      blockade.SecretResolver
	Middleware    func(http.Handler) http.Handler
}

// Run executes a content subcommand through Cymonkey's public invocation.
func (engine CommandEngine) Run(args []string, stdout, stderr io.Writer) error {
	invocation := strings.TrimSpace(engine.Invocation)
	if invocation == "" {
		return errors.New("content command engine requires an invocation name")
	}
	defaultConfig := strings.TrimSpace(engine.DefaultConfig)
	if defaultConfig == "" {
		var err error
		defaultConfig, err = DefaultConfigPath()
		if err != nil {
			return err
		}
	}
	defaultBind := strings.TrimSpace(engine.DefaultBind)
	if defaultBind == "" {
		defaultBind = envOrDefault("CYMONKEY_CONTENT_BIND", "127.0.0.1:8091")
	}
	registry := engine.Registry
	if registry == nil {
		registry = blockade.NewProviderAdapterRegistry()
	}
	return run(args, stdout, stderr, registry, engine.Resolver, invocation, defaultConfig, defaultBind, engine.Middleware)
}

func run(args []string, stdout, stderr io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver, invocation, defaultConfig, defaultBind string, middleware func(http.Handler) http.Handler) error {
	if len(args) == 0 {
		return fmt.Errorf("%s requires endpoint, model, runtime, validate, observe, interpret, or serve", invocation)
	}
	switch args[0] {
	case "validate":
		return validateCommand(args[1:], stdout, invocation, defaultConfig)
	case "endpoint":
		return endpointCommand(args[1:], stdout, registry, resolver, invocation, defaultConfig)
	case "model":
		return modelCommand(args[1:], stdout, registry, resolver, invocation, defaultConfig)
	case "runtime":
		return runtimeCommand(args[1:], stdout, invocation)
	case "observe":
		return observeCommand(args[1:], stdout, registry, resolver, invocation, defaultConfig)
	case "interpret":
		return interpretCommand(args[1:], stdout, registry, resolver, invocation, defaultConfig)
	case "serve":
		return serveCommand(args[1:], stderr, registry, resolver, invocation, defaultConfig, defaultBind, middleware)
	case "help", "-h", "--help":
		printUsage(stderr, invocation)
		return nil
	default:
		return fmt.Errorf("unknown %s command %q", invocation, args[0])
	}
}

func printUsage(output io.Writer, invocation string) {
	fmt.Fprintf(output, `Usage: %s <command>

Commands:
  endpoint   Configure and test model endpoints
  model      Install and manage local model artifacts
  runtime    Install and manage source-provider runtimes
  validate   Validate the content configuration
  observe    Run image observation with a configured adapter
  interpret  Interpret text, image, audio, or video content
  serve      Serve the selected adapter over HTTP or HTTPS`, invocation)
}

func runtimeCommand(args []string, stdout io.Writer, invocation string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s runtime requires manifest, install, list, or remove", invocation)
	}
	switch args[0] {
	case "manifest":
		flags := flag.NewFlagSet(invocation+" runtime manifest", flag.ContinueOnError)
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
			return fmt.Errorf("%s runtime manifest requires --source, --name, --version, --executable, and --output", invocation)
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
		flags := flag.NewFlagSet(invocation+" runtime install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifestPath := flags.String("manifest", "", "local runtime.json path")
		runtimeRoot := flags.String("root", "", "installed runtime directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifestPath) == "" {
			return fmt.Errorf("%s runtime install requires --manifest", invocation)
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
		flags := flag.NewFlagSet(invocation+" runtime list", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		runtimeRoot := flags.String("root", "", "installed runtime directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("%s runtime list accepts flags only", invocation)
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
		flags := flag.NewFlagSet(invocation+" runtime remove", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		name := flags.String("name", "", "installed runtime name")
		version := flags.String("version", "", "installed runtime version")
		runtimeRoot := flags.String("root", "", "installed runtime directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*name) == "" || strings.TrimSpace(*version) == "" {
			return fmt.Errorf("%s runtime remove requires --name and --version", invocation)
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
		return fmt.Errorf("unknown %s runtime command %q", invocation, args[0])
	}
}

func modelCommand(args []string, stdout io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver, invocation, defaultConfig string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s model requires install, list, or remove", invocation)
	}
	switch args[0] {
	case "install":
		flags := flag.NewFlagSet(invocation+" model install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifestPath := flags.String("manifest", "", "local model.json path")
		configPath := flags.String("config", defaultConfig, "content YAML config containing the adapter")
		providerID := flags.String("provider", "", "provider adapter id when config has more than one matching provider")
		modelRoot := flags.String("root", "", "installed model directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifestPath) == "" {
			return fmt.Errorf("%s model install requires --manifest", invocation)
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
		flags := flag.NewFlagSet(invocation+" model list", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		modelRoot := flags.String("root", "", "installed model directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("%s model list accepts flags only", invocation)
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
		flags := flag.NewFlagSet(invocation+" model remove", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		provider := flags.String("provider", "", "installed provider name")
		model := flags.String("model", "", "installed model reference name@version")
		modelRoot := flags.String("root", "", "installed model directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*provider) == "" || strings.TrimSpace(*model) == "" {
			return fmt.Errorf("%s model remove requires --provider and --model", invocation)
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
		return fmt.Errorf("unknown %s model command %q", invocation, args[0])
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

func interpretCommand(args []string, stdout io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver, invocation, defaultConfig string) error {
	flags := flag.NewFlagSet(invocation+" interpret", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig, "content YAML config")
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
	if flags.NArg() != 0 {
		return fmt.Errorf("%s interpret accepts flags only", invocation)
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

func validateCommand(args []string, stdout io.Writer, invocation, defaultConfig string) error {
	flags := flag.NewFlagSet(invocation+" validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig, "content YAML config")
	checkFiles := flags.Bool("check-files", false, "verify local model files exist")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s validate accepts flags only", invocation)
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
	_, err = fmt.Fprintf(stdout, "valid content config: %d adapter(s)\n", len(config.Providers))
	return err
}

func observeCommand(args []string, stdout io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver, invocation, defaultConfig string) error {
	flags := flag.NewFlagSet(invocation+" observe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig, "content YAML config")
	inferenceID := flags.String("inference", "", "provider instance id")
	imagePath := flags.String("image", "", "PNG or JPEG image path")
	prompt := flags.String("prompt", "", "optional observation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s observe accepts flags only", invocation)
	}
	if strings.TrimSpace(*imagePath) == "" {
		return errors.New("--image is required")
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

func serveCommand(args []string, stderr io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver, invocation, defaultConfig, defaultBind string, middleware func(http.Handler) http.Handler) error {
	flags := flag.NewFlagSet(invocation+" serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defaultConfig, "content YAML config")
	inferenceID := flags.String("inference", "", "provider instance id")
	bind := flags.String("bind", defaultBind, "content service HTTP bind address")
	tokenEnvironment := flags.String("token-env", "", "environment variable containing the required bearer token")
	tlsCertificate := flags.String("tls-cert", "", "TLS certificate path")
	tlsKey := flags.String("tls-key", "", "TLS private key path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s serve accepts flags only", invocation)
	}
	bindHost, _, err := net.SplitHostPort(*bind)
	if err != nil {
		return errors.New("--bind must be a host:port address")
	}
	if !loopbackHost(bindHost) && (strings.TrimSpace(*tokenEnvironment) == "" || *tlsCertificate == "" || *tlsKey == "") {
		return errors.New("non-loopback content service requires --token-env, --tls-cert, and --tls-key")
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
	handler := service.Routes()
	if middleware != nil {
		handler = middleware(handler)
	}
	if name := strings.TrimSpace(*tokenEnvironment); name != "" {
		token, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(token) == "" {
			return fmt.Errorf("content bearer token environment variable %q is not set", name)
		}
		handler = blockade.RequireBearerToken(handler, token)
	}
	if (*tlsCertificate == "") != (*tlsKey == "") {
		return fmt.Errorf("%s serve requires --tls-cert and --tls-key together", invocation)
	}
	server := &http.Server{Addr: *bind, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	scheme := "http"
	if *tlsCertificate != "" {
		scheme = "https"
	}
	_, _ = fmt.Fprintf(stderr, "content service listening on %s://%s\n", scheme, *bind)
	var serveErr error
	if *tlsCertificate != "" {
		serveErr = server.ListenAndServeTLS(*tlsCertificate, *tlsKey)
	} else {
		serveErr = server.ListenAndServe()
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("serve content: %w", serveErr)
	}
	return nil
}

func selectProvider(config blockade.Config, inferenceID string) (blockade.ProviderAdapterConfig, error) {
	inferenceID = strings.TrimSpace(inferenceID)
	if inferenceID == "" {
		selected, ok := config.DefaultProvider()
		if !ok {
			return blockade.ProviderAdapterConfig{}, errors.New("content config has no adapter")
		}
		return selected, nil
	}
	selected, ok := config.Provider(inferenceID)
	if !ok {
		return blockade.ProviderAdapterConfig{}, fmt.Errorf("content adapter %q is not configured", inferenceID)
	}
	return selected, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func loopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
