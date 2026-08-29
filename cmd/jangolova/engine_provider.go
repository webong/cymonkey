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
	"strings"
	"syscall"
	"time"

	"jangolova/internal/blockade"
	"jangolova/internal/builtin"
	"jangolova/internal/engineprovider"
	"jangolova/internal/orchestrator"
)

func serveEngineProviderCommand(args []string) error {
	flags := flag.NewFlagSet("serve-engine-provider", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	bind := flags.String(
		"bind",
		envOrDefault("JANGOLOVA_PROVIDER_BIND", "127.0.0.1:7391"),
		"interaction-provider HTTP bind address",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("serve-engine-provider accepts flags only")
	}
	token := strings.TrimSpace(os.Getenv("JANGOLOVA_PROVIDER_TOKEN"))
	if token == "" {
		return errors.New("JANGOLOVA_PROVIDER_TOKEN is required")
	}
	registry, err := builtin.EngineRegistry()
	if err != nil {
		return err
	}
	provider, err := newEngineProvider(registry, token)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	server := &http.Server{
		Addr:              *bind,
		Handler:           provider.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			15*time.Second,
		)
		defer cancel()
		_ = provider.Close(shutdownCtx)
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(os.Stderr, "jangolova interaction provider listening on %s\n", *bind)
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve interaction provider: %w", err)
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func newEngineProvider(registry *orchestrator.Registry, token string) (*engineprovider.Service, error) {
	var options []engineprovider.ServiceOption
	endpoint := strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_ENDPOINT"))
	configPath := strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_CONFIG"))
	if endpoint != "" && configPath != "" {
		return nil, errors.New("JANGOLOVA_BLOCKADE_ENDPOINT and JANGOLOVA_BLOCKADE_CONFIG are mutually exclusive")
	}
	if configPath != "" {
		config, err := blockade.LoadConfig(configPath)
		if err != nil {
			return nil, fmt.Errorf("load embedded Blockade config: %w", err)
		}
		engineID := strings.TrimSpace(os.Getenv("JANGOLOVA_BLOCKADE_ENGINE"))
		if engineID == "" {
			engineID = config.Engines[0].ID
		}
		engineConfig, ok := config.Engine(engineID)
		if !ok {
			return nil, fmt.Errorf("embedded Blockade engine %q is not configured", engineID)
		}
		engine, err := blockade.StartConfiguredEngine(context.Background(), engineConfig)
		if err != nil {
			return nil, fmt.Errorf("start embedded Blockade engine %q: %w", engineID, err)
		}
		options = append(options, engineprovider.WithBlockadeEngine(engine))
	}
	if endpoint != "" {
		options = append(options, engineprovider.WithBlockadeClient(blockade.Client{BaseURL: endpoint}))
	}
	return engineprovider.NewService(registry, token, options...)
}
