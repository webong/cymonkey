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
	targetID := flags.String("target", "", "ID returned by cymonkey browser targets")
	bundleID := flags.String("bundle-id", "", "caller-owned Safari app bundle ID")
	appName := flags.String("app-name", "", "caller-owned Safari app name")
	revision := flags.String("revision", "", "revision returned by extension inspect")
	executable := flags.String("browser-bin", "", "absolute path to the selected browser executable")
	profile := flags.String("profile", "", "absolute path to Chromium user data directory or Firefox profile")
	profileDirectory := flags.String("profile-directory", "", "profile directory within the browser user data directory")
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
			target, resolveErr := resolveActionTarget(input)
			if resolveErr != nil {
				return resolveErr
			}
			return installNativeExtension(ctx, target, input.Source, input.Destination, input.Revision, stdout)
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
		target, resolveErr := resolveCLITarget(*targetID, *browserName, *executable, *profile, *profileDirectory)
		if resolveErr != nil {
			return resolveErr
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return installNativeExtension(ctx, target, *source, *destination, *revision, stdout)
	case "run":
		target, resolveErr := resolveCLITarget(*targetID, *browserName, *executable, *profile, *profileDirectory)
		if resolveErr != nil {
			return resolveErr
		}
		target.Headless = *headless
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return browserextension.RunWithDevTools(ctx, target, *source, *destination, *revision, func(result browserextension.InstallResult) error {
			return json.NewEncoder(stdout).Encode(result)
		})
	case "capabilities":
		if *targetID != "" {
			target, resolveErr := resolveCLITarget(*targetID, *browserName, *executable, *profile, *profileDirectory)
			if resolveErr != nil {
				return resolveErr
			}
			result = browserextension.Capability(target.Browser)
		} else {
			result = browserextension.Capability(*browserName)
		}
	case "prepare", "inspect":
		result, err = inspectExtensionSource(*source)
	case "package":
		result, err = browserextension.Package(*source, *output)
	case "package-safari":
		result, err = browserextension.PackageSafariProject(context.Background(), *source, *output, *revision, *bundleID, *appName)
	case "stage":
		selectedBrowser := *browserName
		if *targetID != "" {
			target, resolveErr := resolveCLITarget(*targetID, *browserName, *executable, *profile, *profileDirectory)
			if resolveErr != nil {
				return resolveErr
			}
			selectedBrowser = target.Browser
		}
		result, err = browserextension.StageForChromiumWithRevision(selectedBrowser, *source, *destination, *revision)
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
	case "chrome":
		if browserextension.IsDefaultChromeUserDataDir(target.ProfilePath) {
			if revision == "" {
				return errors.New("extension installation requires a prepared revision")
			}
			result, err := browserextension.StageForChromiumWithRevision("chrome", source, destination, revision)
			if err != nil {
				return err
			}
			directory := target.ProfileDirectory
			if directory == "" {
				directory = "Default"
			}
			result.Profile = filepath.Join(target.ProfilePath, directory)
			result.NextAction += " Use the selected Chrome profile: " + result.Profile + "."
			return encoder.Encode(result)
		}
	case "firefox":
		return browserextension.InstallFirefox(ctx, target, source, revision, func(result browserextension.InstallResult) error { return encoder.Encode(result) })
	case "safari":
		if target.ProfilePath != "" || target.ProfileDirectory != "" {
			return errors.New("Safari profile enablement is selected in Safari Settings; do not pass a profile path")
		}
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

func resolveCLITarget(id, browser, executable, profile, directory string) (browserextension.DevToolsTarget, error) {
	if id != "" {
		if browser != "" || executable != "" || profile != "" || directory != "" {
			return browserextension.DevToolsTarget{}, errors.New("--target cannot be combined with --browser, --browser-bin, --profile, or --profile-directory")
		}
		selected, err := browserextension.ResolveTarget(id)
		if err != nil {
			return browserextension.DevToolsTarget{}, err
		}
		return selected.DevToolsTarget(), nil
	}
	if browser == "" {
		return browserextension.DevToolsTarget{}, errors.New("select --target or --browser with explicit paths")
	}
	if (browser == "chrome" || browser == "chromium" || browser == "edge") && directory == "" {
		directory = "Default"
	}
	return browserextension.DevToolsTarget{Browser: browser, ExecutablePath: executable, ProfilePath: profile, ProfileDirectory: directory}, nil
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
	Headless          *bool  `json:"headless,omitempty"`
	Target            struct {
		ID               string `json:"id"`
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
	if input.Target.ID != "" && (input.Target.Browser != "" || input.Target.ExecutablePath != "" || input.Target.ProfilePath != "" || input.Target.ProfileDirectory != "") {
		return input, errors.New("target.id cannot be combined with explicit browser or profile fields")
	}
	return input, nil
}

func resolveActionTarget(input extensionActionInput) (browserextension.DevToolsTarget, error) {
	if input.Target.ID != "" {
		selected, err := browserextension.ResolveTarget(input.Target.ID)
		if err != nil {
			return browserextension.DevToolsTarget{}, err
		}
		return selected.DevToolsTarget(), nil
	}
	return resolveCLITarget("", input.Target.Browser, input.Target.ExecutablePath, input.Target.ProfilePath, input.Target.ProfileDirectory)
}

// extensionAction handles the one-shot structured actions used by local tools.
// The long-running extension.install action is dispatched by extensionCommand.
func extensionAction(name string, raw []byte) (any, error) {
	input, err := parseExtensionActionInput(raw)
	if err != nil {
		return nil, err
	}
	switch name {
	case "extension.targets":
		return browserextension.DiscoverTargets()
	case "extension.capabilities":
		target, err := resolveActionTarget(input)
		if err != nil {
			return nil, err
		}
		return browserextension.Capability(target.Browser), nil
	case "extension.prepare":
		return inspectExtensionSource(input.Source)
	case "extension.package":
		return browserextension.Package(input.Source, input.Output)
	case "extension.package-safari":
		return browserextension.PackageSafariProject(context.Background(), input.Source, input.Output, input.Revision, input.BundleID, input.AppName)
	case "extension.install":
		return nil, errors.New("extension.install is long-running; invoke it through the cymonkey extension act command")
	case "extension.stage":
		target, err := resolveActionTarget(input)
		if err != nil {
			return nil, err
		}
		return browserextension.StageForChromiumWithRevision(target.Browser, input.Source, input.Destination, input.Revision)
	case "extension.install-store":
		return browserextension.RequestChromeWebStoreInstall(input.ID, input.ExternalDirectory)
	default:
		return nil, errors.New("unsupported extension manager action")
	}
}
