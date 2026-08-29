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

	"jangolova/internal/builtin"
	"jangolova/internal/engineprovider"
)

func serveMCPCommand(args []string) error {
	flags := flag.NewFlagSet("serve-mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	bind := flags.String("bind", "", "optional MCP Streamable HTTP bind address; empty uses stdio")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("serve-mcp accepts flags only")
	}
	token := strings.TrimSpace(os.Getenv("JANGOLOVA_PROVIDER_TOKEN"))
	if token == "" {
		return errors.New("JANGOLOVA_PROVIDER_TOKEN is required")
	}
	registry, err := builtin.EngineRegistry()
	if err != nil {
		return err
	}
	provider, err := engineprovider.NewService(registry, token)
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
	fmt.Fprintf(os.Stderr, "jangolova MCP tool server listening on %s\\n", *bind)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve Jangolova MCP: %w", err)
	}
	return nil
}
