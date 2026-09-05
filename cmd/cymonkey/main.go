package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"cymonkey/internal/cymonkeyhost"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "cymonkey: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("cymonkey requires validate or run")
	}
	switch args[0] {
	case "validate":
		return validate(args[1:], stdout)
	case "run":
		return runHost(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stderr)
		return nil
	default:
		return fmt.Errorf("unknown cymonkey command %q", args[0])
	}
}

func validate(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("cymonkey validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", envOrDefault("CYMONKEY_CONFIG", ""), "Cymonkey host YAML config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cymonkey validate accepts flags only")
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}
	config, err := cymonkeyhost.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "valid Cymonkey host config: %d component(s)\n", len(config.Components))
	return err
}

func runHost(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("cymonkey run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", envOrDefault("CYMONKEY_CONFIG", ""), "Cymonkey host YAML config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cymonkey run accepts flags only")
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}
	config, err := cymonkeyhost.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	supervisor, err := cymonkeyhost.NewSupervisor(config, stdout, stderr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, _ = fmt.Fprintf(stderr, "cymonkey host running %d component(s)\n", len(config.Components))
	return supervisor.Run(ctx)
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: cymonkey <command>

Commands:
  validate                Validate a Cymonkey host manifest
  run                     Start and supervise standalone components`)
}
