package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"jangolova/internal/blockade"
	"jangolova/internal/builtin"
	"jangolova/internal/grimlock"
	"jangolova/targetconn"
)

// grimlockCommand is the ergonomic alias for the Grimlock servers. The long
// serve-grimlock* commands remain supported for scripts and compatibility.
func grimlockCommand(args []string) error {
	mode := "http"
	forwarded := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--mcp":
			if mode != "http" {
				return errors.New("grimlock protocol flags --mcp and --acp are mutually exclusive")
			}
			mode = "mcp"
		case "--acp":
			if mode != "http" {
				return errors.New("grimlock protocol flags --mcp and --acp are mutually exclusive")
			}
			mode = "acp"
		default:
			forwarded = append(forwarded, arg)
		}
	}
	switch mode {
	case "mcp":
		return serveGrimlockMCPCommand(forwarded)
	case "acp":
		return serveGrimlockACPCommand(forwarded)
	default:
		return serveGrimlockCommand(forwarded)
	}
}

func serveGrimlockCommand(args []string) error {
	flags := flag.NewFlagSet("serve-grimlock", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	bind := flags.String(
		"bind",
		envOrDefault("JANGOLOVA_GRIMLOCK_BIND", "127.0.0.1:7392"),
		"Grimlock HTTP bind address",
	)
	storeDir := flags.String("session-store", strings.TrimSpace(os.Getenv("JANGOLOVA_GRIMLOCK_SESSION_STORE")), "directory for persistent Grimlock session metadata and events")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("serve-grimlock accepts flags only")
	}
	service, err := newGrimlockService(grimlockStoreOption(*storeDir))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Addr: *bind, Handler: service.Routes(), ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = service.Close(shutdownCtx)
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(os.Stderr, "jangolova Grimlock HTTP service listening on %s\n", *bind)
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve Grimlock: %w", err)
	}
	return nil
}

func newGrimlockService(options ...grimlock.ServiceOption) (*grimlock.Service, error) {
	token := strings.TrimSpace(os.Getenv("JANGOLOVA_GRIMLOCK_TOKEN"))
	if token == "" {
		return nil, errors.New("JANGOLOVA_GRIMLOCK_TOKEN is required")
	}
	resolver := targetconn.DefaultResolver()
	runtime, err := grimlock.NewDefaultRuntime(resolver)
	if err != nil {
		return nil, err
	}
	registry, err := builtin.EngineRegistry()
	if err != nil {
		return nil, err
	}
	serviceOptions := []grimlock.ServiceOption{grimlock.WithTargetResolver(resolver)}
	serviceOptions = append(serviceOptions, options...)
	if configPath := strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_CONFIG")); configPath != "" {
		config, configErr := blockade.LoadConfig(configPath)
		if configErr != nil {
			return nil, fmt.Errorf("load Blockade config: %w", configErr)
		}
		engineID := strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_ENGINE"))
		if engineID == "" {
			engineID = config.Engines[0].ID
		}
		engine, ok := config.Engine(engineID)
		if !ok {
			return nil, fmt.Errorf("Blockade engine %q is not configured", engineID)
		}
		if engine.Kind == "local-ultralytics" {
			pool, poolErr := blockade.StartConfiguredLocalEngine(context.Background(), engine)
			if poolErr != nil {
				return nil, fmt.Errorf("start configured Blockade engine: %w", poolErr)
			}
			serviceOptions = append(serviceOptions, grimlock.WithBlockadeWorkerPool(pool))
		}
	}
	if endpoint := strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_ENDPOINT")); endpoint != "" {
		serviceOptions = append(serviceOptions, grimlock.WithBlockadeClient(blockade.Client{BaseURL: endpoint}))
	}
	if strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_CONFIG")) == "" {
		if command := strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_WORKER_COMMAND")); command != "" {
			workers := 1
			if value := strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_WORKERS")); value != "" {
				if parsed, parseErr := strconv.Atoi(value); parseErr == nil {
					workers = parsed
				}
			}
			pool, poolErr := blockade.NewWorkerPool(context.Background(), blockade.WorkerConfig{Command: strings.Fields(command), Workers: workers, Stderr: os.Stderr})
			if poolErr != nil {
				return nil, fmt.Errorf("configure Blockade workers: %w", poolErr)
			}
			serviceOptions = append(serviceOptions, grimlock.WithBlockadeWorkerPool(pool))
		}
	}
	return grimlock.NewService(runtime, registry, token, serviceOptions...)
}

func grimlockStoreOption(dir string) grimlock.ServiceOption {
	if strings.TrimSpace(dir) == "" {
		return func(*grimlock.Service) {}
	}
	return grimlock.WithStoreDirectory(strings.TrimSpace(dir))
}
