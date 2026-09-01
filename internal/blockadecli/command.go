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

	"jangolova/internal/blockade"
)

// Run executes the standalone Blockade command surface. Jangolova retains a
// compatibility subcommand that delegates here.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("blockade requires validate, observe, or serve")
	}
	switch args[0] {
	case "validate":
		return validateCommand(args[1:], stdout)
	case "observe":
		return observeCommand(args[1:], stdout)
	case "serve":
		return serveCommand(args[1:], stderr)
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
	_, err = fmt.Fprintf(stdout, "valid Blockade config: %d engine(s)\n", len(config.Engines))
	return err
}

func observeCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("blockade observe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	engineID := flags.String("engine", "", "engine id")
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
	id := strings.TrimSpace(*engineID)
	if id == "" {
		id = config.Engines[0].ID
	}
	engine, ok := config.Engine(id)
	if !ok {
		return fmt.Errorf("Blockade engine %q is not configured", id)
	}
	started, err := blockade.StartConfiguredEngine(context.Background(), engine)
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

func serveCommand(args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("blockade serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Blockade YAML config")
	engineID := flags.String("engine", "", "engine id")
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
	id := strings.TrimSpace(*engineID)
	if id == "" {
		id = config.Engines[0].ID
	}
	engineConfig, ok := config.Engine(id)
	if !ok {
		return fmt.Errorf("Blockade engine %q is not configured", id)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	engine, err := blockade.StartConfiguredEngine(ctx, engineConfig)
	if err != nil {
		return err
	}
	defer engine.Close()
	service, err := blockade.NewHTTPServer(engine, id)
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

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
