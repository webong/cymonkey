package provider

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

	"cymonkey/src/internal/engineprovider"
	"cymonkey/src/internal/orchestrator"
)

func serveEngineProviderCommand(args []string, registryFactory RegistryFactory) error {
	flags := flag.NewFlagSet("display serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	bind := flags.String(
		"bind",
		envOrDefault("CYMONKEY_DISPLAY_BIND", "127.0.0.1:7391"),
		"display service HTTP bind address",
	)
	tokenEnvironment := flags.String("token-env", "CYMONKEY_DISPLAY_TOKEN", "environment variable containing the display service token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("display serve accepts flags only")
	}
	token := strings.TrimSpace(os.Getenv(strings.TrimSpace(*tokenEnvironment)))
	if token == "" {
		return fmt.Errorf("display token environment variable %q is required", *tokenEnvironment)
	}
	registry, err := registryFrom(registryFactory)
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
	fmt.Fprintf(os.Stderr, "display interaction service listening on %s\n", *bind)
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve display service: %w", err)
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
