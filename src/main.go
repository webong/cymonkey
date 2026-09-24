package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"blockade"
	blockadecli "blockade/cli"
	blockadewebllm "blockade/webllm"
	nativebridge "cymonkey/src/fixtures/native-bridge"
	"cymonkey/src/host"
	"cymonkey/src/provider"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "cymonkey: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("cymonkey requires a command: validate, run, observe, modules, extension, provider, blockade, or native-bridge-fixture")
	}
	switch args[0] {
	case "validate":
		return validate(args[1:], stdout)
	case "run":
		return runHost(args[1:], stdout, stderr)
	case "observe":
		return observe(args[1:], stdout)
	case "modules":
		return modulesCommand(args[1:], stdout, stderr)
	case "extension":
		return extensionCommand(args[1:], stdout)
	case "provider":
		return provider.Run(args[1:], stderr)
	case "blockade":
		return runBlockade(args[1:], stdout, stderr)
	case "native-bridge-fixture":
		nativebridge.Run()
		return nil
	case "help", "-h", "--help":
		printUsage(stderr)
		return nil
	default:
		return fmt.Errorf("unknown cymonkey command %q", args[0])
	}
}

func observe(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("cymonkey observe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", envOrDefault("CYMONKEY_CONFIG", ""), "Cymonkey host YAML config")
	instanceID := flags.String("instance", "", "Jangolova interaction instance ID")
	prompt := flags.String("prompt", "", "optional observation prompt")
	fullPage := flags.Bool("full-page", false, "capture the full browser page")
	approvalID := flags.String("approval-id", "", "optional Jangolova screenshot approval")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cymonkey observe accepts flags only")
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}
	if strings.TrimSpace(*instanceID) == "" {
		return errors.New("--instance is required")
	}
	config, err := cymonkeyhost.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if config.Observation == nil {
		return errors.New("Cymonkey host config has no observation coordinator")
	}
	coordinator, err := cymonkeyhost.NewObservationCoordinator(*config.Observation)
	if err != nil {
		return err
	}
	result, err := coordinator.Observe(context.Background(), cymonkeyhost.ObservationRequest{
		InstanceID: *instanceID, Prompt: *prompt, FullPage: *fullPage, ApprovalID: *approvalID,
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
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

func runBlockade(args []string, stdout, stderr io.Writer) error {
	registry := blockade.NewProviderAdapterRegistry()
	if err := blockadewebllm.Register(registry); err != nil {
		return err
	}
	return blockadecli.RunWithProviderAdapters(args, stdout, stderr, registry, blockade.EnvironmentSecretResolver{})
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
  run                     Start and supervise standalone components
  observe                 Coordinate a Jangolova screenshot with Blockade
  modules                 Discover or retrieve reviewed Jangolova modules
  extension               Manage caller-supplied browser extension packages
  provider                Run the Jangolova provider/MCP interface
  blockade                Run the Blockade inference interface
  native-bridge-fixture   Run the native bridge fixture`)
}
