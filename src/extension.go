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
	"path/filepath"
	"strings"
	"syscall"

	"jangolova/browserextension"
)

// extensionCommand is Cymonkey's manager entry point for extensions supplied
// by external callers. Jangolova implements the browser-specific operations.
func extensionCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("extension requires act, capabilities, prepare, inspect, package, package-safari, install, run, stage, or install-store")
	}
	flags := flag.NewFlagSet("cymonkey extension "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "extension directory, ZIP, signed XPI, or Safari app")
	output := flags.String("output", "", "output ZIP or Safari project location")
	destination := flags.String("destination", "", "staged extension directory")
	browserName := flags.String("browser", "", "selected browser: chrome, chromium, edge, firefox, or safari")
	bundleID := flags.String("bundle-id", "", "caller-owned Safari app bundle ID")
	appName := flags.String("app-name", "", "caller-owned Safari app name")
	revision := flags.String("revision", "", "revision returned by extension inspect")
	executable := flags.String("browser-bin", "", "absolute path to the selected browser executable")
	profile := flags.String("profile", "", "absolute path to Chromium user data directory or Firefox profile")
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
				ProfilePath: input.Target.ProfilePath, ProfileDirectory: input.Target.ProfileDirectory,
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
		result, err = inspectExtensionSource(*source)
	case "package":
		result, err = browserextension.Package(*source, *output)
	case "package-safari":
		result, err = browserextension.PackageSafariProject(context.Background(), *source, *output, *revision, *bundleID, *appName)
	case "stage":
		result, err = browserextension.StageForChromiumWithRevision(*browserName, *source, *destination, *revision)
	case "install-store":
		result, err = browserextension.RequestChromeWebStoreInstall(*id, *externalDirectory)
	default:
		return errors.New("extension requires act, capabilities, prepare, inspect, package, package-safari, install, run, stage, or install-store")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func installNativeExtension(ctx context.Context, target browserextension.DevToolsTarget, source, destination, revision string, output io.Writer) error {
	encoder := json.NewEncoder(output)
	switch target.Browser {
	case "firefox":
		return browserextension.InstallFirefox(ctx, target, source, revision, func(result browserextension.InstallResult) error { return encoder.Encode(result) })
	case "safari":
		result, err := browserextension.InstallSafariApp(ctx, source, revision)
		if err != nil {
			return err
		}
		return encoder.Encode(result)
	}
	return browserextension.InstallWithNativeUI(ctx, target, source, destination, revision, func(result browserextension.InstallResult) error {
		return encoder.Encode(result)
	})
}

func inspectExtensionSource(source string) (browserextension.Description, error) {
	if strings.EqualFold(filepath.Ext(source), ".app") {
		return browserextension.InspectSafariApp(context.Background(), source)
	}
	return browserextension.Inspect(source)
}

type extensionActionInput struct {
	Source            string `json:"source"`
	Output            string `json:"output"`
	Destination       string `json:"destination"`
	Revision          string `json:"revision"`
	ID                string `json:"id"`
	ExternalDirectory string `json:"externalDirectory"`
	BundleID          string `json:"bundleId"`
	AppName           string `json:"appName"`
	Target            struct {
		Browser          string `json:"browser"`
		ExecutablePath   string `json:"executablePath"`
		UserDataDir      string `json:"userDataDir"`
		ProfilePath      string `json:"profilePath"`
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
	if input.Target.ProfilePath != "" && input.Target.UserDataDir != "" && input.Target.ProfilePath != input.Target.UserDataDir {
		return input, errors.New("target.profilePath and target.userDataDir must identify the same profile")
	}
	if input.Target.ProfilePath == "" {
		input.Target.ProfilePath = input.Target.UserDataDir
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
		return inspectExtensionSource(input.Source)
	case "extension.package":
		return browserextension.Package(input.Source, input.Output)
	case "extension.package-safari":
		return browserextension.PackageSafariProject(context.Background(), input.Source, input.Output, input.Revision, input.BundleID, input.AppName)
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
