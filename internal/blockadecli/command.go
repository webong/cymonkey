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

	"cymonkey/internal/blockade"
)

// Run executes the standalone Blockade command surface with no provider
// packages registered. A distribution containing provider integrations can
// call RunWithProviderAdapters with its registry.
func Run(args []string, stdout, stderr io.Writer) error {
	return RunWithProviderAdapters(args, stdout, stderr, blockade.NewProviderAdapterRegistry(), blockade.EnvironmentSecretResolver{})
}

func RunWithProviderAdapters(args []string, stdout, stderr io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver) error {
	if len(args) == 0 {
		return errors.New("blockade requires validate, observe, or serve")
	}
	switch args[0] {
	case "validate":
		return validateCommand(args[1:], stdout)
	case "observe":
		return observeCommand(args[1:], stdout, registry, resolver)
	case "serve":
		return serveCommand(args[1:], stderr, registry, resolver)
	default:
		return fmt.Errorf("unknown blockade command %q", args[0])
	}
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
	_, err = fmt.Fprintf(stdout, "valid Blockade config: %d engine(s), %d provider adapter(s)\n", len(config.Engines), len(config.ProviderAdapters))
	return err
}

func observeCommand(args []string, stdout io.Writer, registry *blockade.ProviderAdapterRegistry, resolver blockade.SecretResolver) error {
	flags := flag.NewFlagSet("blockade observe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	inferenceID := flags.String("inference", "", "engine or provider adapter id")
	engineID := flags.String("engine", "", "deprecated alias for --inference")
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
	selected, err := selectInference(config, *inferenceID, *engineID)
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
	inferenceID := flags.String("inference", "", "engine or provider adapter id")
	engineID := flags.String("engine", "", "deprecated alias for --inference")
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
	selected, err := selectInference(config, *inferenceID, *engineID)
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
	service, err := blockade.NewHTTPServer(engine, selected.ID())
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

func selectInference(config blockade.Config, inferenceID, legacyEngineID string) (blockade.InferenceConfig, error) {
	inferenceID = strings.TrimSpace(inferenceID)
	legacyEngineID = strings.TrimSpace(legacyEngineID)
	if inferenceID != "" && legacyEngineID != "" {
		return blockade.InferenceConfig{}, errors.New("use only one of --inference or --engine")
	}
	id := inferenceID
	if id == "" {
		id = legacyEngineID
	}
	if id == "" {
		selected, ok := config.DefaultInference()
		if !ok {
			return blockade.InferenceConfig{}, errors.New("Blockade config has no inference backend")
		}
		return selected, nil
	}
	selected, ok := config.Inference(id)
	if !ok {
		return blockade.InferenceConfig{}, fmt.Errorf("Blockade inference %q is not configured", id)
	}
	return selected, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
