package devicecli

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

	"board/vncgateway"
)

func gatewayCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "serve" {
		return errors.New("cymy device gateway requires serve")
	}
	flags := flag.NewFlagSet("cymy device gateway serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	bind := flags.String("bind", envValue("CYMONKEY_DEVICE_GATEWAY_BIND", "127.0.0.1:6080"), "gateway bind address")
	upstream := flags.String("upstream", envValue("CYMONKEY_DISPLAY_UPSTREAM", "127.0.0.1:5999"), "private display protocol upstream")
	tokenEnvironment := flags.String("token-env", "CYMONKEY_DEVICE_GATEWAY_TOKEN", "environment variable containing the gateway token")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*bind) == "" || strings.TrimSpace(*upstream) == "" {
		return errors.New("cymy device gateway serve accepts --bind, --upstream, and --token-env")
	}
	tokenName := strings.TrimSpace(*tokenEnvironment)
	token := strings.TrimSpace(os.Getenv(tokenName))
	if tokenName == "" || token == "" {
		return fmt.Errorf("device gateway token environment variable %q is required", tokenName)
	}
	service := &http.Server{
		Addr:              strings.TrimSpace(*bind),
		Handler:           (vncgateway.Server{Token: token, Upstream: strings.TrimSpace(*upstream)}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = service.Shutdown(shutdown)
	}()
	_, _ = fmt.Fprintf(stderr, "device display gateway listening on %s\n", service.Addr)
	if err := service.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve device display gateway: %w", err)
	}
	return nil
}

func envValue(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
