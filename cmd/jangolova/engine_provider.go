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

	"cymonkey/internal/builtin"
	"cymonkey/internal/engineprovider"
	"cymonkey/internal/orchestrator"
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
	return engineprovider.NewService(registry, token)
}
