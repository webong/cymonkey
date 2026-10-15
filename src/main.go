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

	nativebridge "cymonkey/src/fixtures/native-bridge"
	"cymonkey/src/internal/host"
	"cymonkey/src/internal/provider"
	"cymonkey/src/observation"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "cymy: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("cymy requires a command: validate, run, observe, modules, browser, extension, userscript, bookmarklet, display, content, device, operator, plugins, adapters, or cookies")
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
	case "userscript":
		return userscriptCommand(args[1:], stdout)
	case "bookmarklet":
		return bookmarkletCommand(args[1:], stdout)
	case "browser":
		return browserCommand(args[1:], stdout)
	case "display":
		return provider.Run("cymy display", args[1:], stderr, engineRegistry)
	case "operator":
		return operatorCommand(args[1:], stdout, stderr)
	case "device":
		return runDevice(args[1:], stdout, stderr)
	case "content":
		return runContent(args[1:], stdout, stderr)
	case "console":
		return consoleCommand(args[1:], stdout, stderr)
	case "plugins":
		return pluginsCommand(args[1:], stdout, stderr)
	case "adapters":
		return adaptersCommand(args[1:], stdout, stderr)
	case "cookies":
		return cookiesCommand(args[1:], stdout)
	case "native-bridge-fixture":
		nativebridge.Run()
		return nil
	case "help", "-h", "--help":
		printUsage(stderr)
		return nil
	default:
		return fmt.Errorf("unknown cymy command %q", args[0])
	}
}

func observe(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("cymy observe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", envOrDefault("CYMONKEY_CONFIG", ""), "Cymonkey host YAML config")
	instanceID := flags.String("instance", "", "display interaction instance ID")
	prompt := flags.String("prompt", "", "optional observation prompt")
	fullPage := flags.Bool("full-page", false, "capture the full browser page")
	approvalID := flags.String("approval-id", "", "optional display screenshot approval")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cymy observe accepts flags only")
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
	capture, ok := config.Service(observation.CaptureServiceContract)
	if !ok {
		return fmt.Errorf("Cymonkey host config has no %s service", observation.CaptureServiceContract)
	}
	interpretation, ok := config.Service(observation.InterpretationServiceContract)
	if !ok {
		return fmt.Errorf("Cymonkey host config has no %s service", observation.InterpretationServiceContract)
	}
	coordinator, err := observation.NewObservationCoordinator(observation.Config{
		Capture:        observation.ServiceConfig{Endpoint: capture.Endpoint, TokenEnvironment: capture.TokenEnvironment},
		Interpretation: observation.ServiceConfig{Endpoint: interpretation.Endpoint, TokenEnvironment: interpretation.TokenEnvironment},
	})
	if err != nil {
		return err
	}
	result, err := coordinator.Observe(context.Background(), observation.ObservationRequest{
		InstanceID: *instanceID, Prompt: *prompt, FullPage: *fullPage, ApprovalID: *approvalID,
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func validate(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("cymy validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", envOrDefault("CYMONKEY_CONFIG", ""), "Cymonkey host YAML config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cymy validate accepts flags only")
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
	flags := flag.NewFlagSet("cymy run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", envOrDefault("CYMONKEY_CONFIG", ""), "Cymonkey host YAML config")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cymy run accepts flags only")
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
	_, _ = fmt.Fprintf(stderr, "Cymonkey host running %d component(s)\n", len(config.Components))
	return supervisor.Run(ctx)
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: cymy <command>

Commands:
  validate                Validate a Cymonkey host manifest
  run                     Start and supervise standalone components
  observe                 Capture a display and interpret its content
  modules                 Discover or retrieve reviewed display modules
  browser                 Discover local browser targets
  extension               Manage caller-supplied browser extension packages
  userscript              Store and replay approved extension-free userscripts
  bookmarklet             Import or export user-activated browser bookmarklets
  display                 Connect and serve display interaction engines
  content                 Configure and run content interpretation
  console                 Compose selected tools and models; serve HTTP or MCP
  device                  Use approved input and output devices
  operator                Serve or call the unified Cymonkey operator API
  plugins                 Build, install, and inspect Cymonkey plugins`)
}
