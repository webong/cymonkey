package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"

	"jangolova/browserextension"
)

// extensionCommand is Cymonkey's manager entry point for extensions supplied
// by external callers. Jangolova implements the browser-specific operations.
func extensionCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("extension requires act, capabilities, prepare, inspect, package, install, run, stage, or install-store")
	}
	flags := flag.NewFlagSet("cymonkey extension "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "extension directory or ZIP")
	output := flags.String("output", "", "output ZIP path")
	destination := flags.String("destination", "", "staged extension directory")
	browserName := flags.String("browser", "", "selected browser: chrome, chromium, or edge")
	revision := flags.String("revision", "", "revision returned by extension inspect")
	executable := flags.String("browser-bin", "", "absolute path to the selected browser executable")
	profile := flags.String("profile", "", "absolute path to the browser user data directory")
	profileDirectory := flags.String("profile-directory", "Default", "profile directory within the browser user data directory")
	headless := flags.Bool("headless", true, "run a session-only extension load without a browser window")
	id := flags.String("id", "", "Chrome Web Store extension ID")
	externalDirectory := flags.String("external-dir", "", "Chrome External Extensions directory")
	name := flags.String("name", "", "extension manager action name")
	input := flags.String("input", "{}", "JSON input for extension act")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("extension command accepts flags only")
	}
	var result any
	var err error
	switch args[0] {
	case "act":
		if *name == "extension.install" {
			input, parseErr := parseExtensionActionInput([]byte(*input))
			if parseErr != nil {
				return parseErr
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return installNativeExtension(ctx, browserextension.DevToolsTarget{
				Browser: input.Target.Browser, ExecutablePath: input.Target.ExecutablePath,
				ProfilePath: input.Target.UserDataDir, ProfileDirectory: input.Target.ProfileDirectory,
			}, input.Source, input.Destination, input.Revision, stdout)
		}
		result, err = extensionAction(*name, []byte(*input))
	case "install":
		flags.Visit(func(item *flag.Flag) {
			if item.Name == "headless" && *headless {
				err = errors.New("native extension installation requires a visible browser")
			}
		})
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return installNativeExtension(ctx, browserextension.DevToolsTarget{
			Browser: *browserName, ExecutablePath: *executable,
			ProfilePath: *profile, ProfileDirectory: *profileDirectory,
		}, *source, *destination, *revision, stdout)
	case "run":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return browserextension.RunWithDevTools(ctx, browserextension.DevToolsTarget{
			Browser: *browserName, ExecutablePath: *executable, ProfilePath: *profile,
			ProfileDirectory: *profileDirectory, Headless: *headless,
		}, *source, *destination, *revision, func(result browserextension.InstallResult) error {
			return json.NewEncoder(stdout).Encode(result)
		})
	case "capabilities":
		result = browserextension.Capability(*browserName)
	case "prepare", "inspect":
		result, err = browserextension.Inspect(*source)
	case "package":
		result, err = browserextension.Package(*source, *output)
	case "stage":
		result, err = browserextension.StageForChromiumWithRevision(*browserName, *source, *destination, *revision)
	case "install-store":
		result, err = browserextension.RequestChromeWebStoreInstall(*id, *externalDirectory)
	default:
		return errors.New("extension requires act, capabilities, prepare, inspect, package, install, run, stage, or install-store")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func installNativeExtension(ctx context.Context, target browserextension.DevToolsTarget, source, destination, revision string, output io.Writer) error {
	encoder := json.NewEncoder(output)
	return browserextension.InstallWithNativeUI(ctx, target, source, destination, revision, func(result browserextension.InstallResult) error {
		return encoder.Encode(result)
	})
}

type extensionActionInput struct {
	Source            string `json:"source"`
	Output            string `json:"output"`
	Destination       string `json:"destination"`
	Revision          string `json:"revision"`
	ID                string `json:"id"`
	ExternalDirectory string `json:"externalDirectory"`
	Target            struct {
		Browser          string `json:"browser"`
		ExecutablePath   string `json:"executablePath"`
		UserDataDir      string `json:"userDataDir"`
		ProfileDirectory string `json:"profileDirectory"`
	} `json:"target"`
}

func parseExtensionActionInput(raw []byte) (extensionActionInput, error) {
	var input extensionActionInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return input, errors.New("extension action input must be one JSON object")
	}
	return input, nil
}

// extensionAction handles the one-shot structured actions used by local tools.
// The long-running extension.install action is dispatched by extensionCommand.
func extensionAction(name string, raw []byte) (any, error) {
	input, err := parseExtensionActionInput(raw)
	if err != nil {
		return nil, err
	}
	switch name {
	case "extension.capabilities":
		return browserextension.Capability(input.Target.Browser), nil
	case "extension.prepare":
		return browserextension.Inspect(input.Source)
	case "extension.package":
		return browserextension.Package(input.Source, input.Output)
	case "extension.install":
		return nil, errors.New("extension.install is long-running; invoke it through the cymonkey extension act command")
	case "extension.stage":
		return browserextension.StageForChromiumWithRevision(input.Target.Browser, input.Source, input.Destination, input.Revision)
	case "extension.install-store":
		return browserextension.RequestChromeWebStoreInstall(input.ID, input.ExternalDirectory)
	default:
		return nil, errors.New("unsupported extension manager action")
	}
}
