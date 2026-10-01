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
)

func serveMCPCommand(args []string, registryFactory RegistryFactory) error {
	flags := flag.NewFlagSet("display serve-tools", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	bind := flags.String("bind", "", "optional MCP Streamable HTTP bind address; empty uses stdio")
	tokenEnvironment := flags.String("token-env", "CYMONKEY_DISPLAY_TOKEN", "environment variable containing the display service token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("display serve-tools accepts flags only")
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
	defer provider.Close(context.Background())
	mcp, err := engineprovider.NewMCPServer(provider)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*bind) == "" {
		return mcp.ServeStdio(context.Background(), os.Stdin, os.Stdout)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: *bind, Handler: mcp.Routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(os.Stderr, "display tool server listening on %s\\n", *bind)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve display tools: %w", err)
	}
	return nil
}
