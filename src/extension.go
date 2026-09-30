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
		return errors.New("extension requires act, capabilities, prepare, inspect, package, sign, package-safari, build-safari, install, run, stage, install-store, remove-store, or policy")
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
	project := flags.String("project", "", "absolute path to a caller-owned Safari Xcode project")
	scheme := flags.String("scheme", "", "Safari Xcode app scheme")
	teamID := flags.String("team-id", "", "caller's Apple developer team ID")
	keyPath := flags.String("key", "", "existing Chromium private key; omit to generate one next to the CRX")
	revision := flags.String("revision", "", "revision returned by extension inspect")
	executable := flags.String("browser-bin", "", "absolute path to the selected browser executable")
	profile := flags.String("profile", "", "absolute path to Chromium user data directory or Firefox profile")
	profileDirectory := flags.String("profile-directory", "", "profile directory within the browser user data directory")
	headless := flags.Bool("headless", true, "run a session-only extension load without a browser window")
	id := flags.String("id", "", "published extension ID")
	store := flags.String("store", "", "store: chrome or edge (defaults to selected browser)")
	policyMode := flags.String("mode", "", "managed policy action: force, unforce, block, or unblock")
	externalDirectory := flags.String("external-dir", "", "macOS or Linux External Extensions directory; defaults to the documented path")
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
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		result, err = extensionActionContext(ctx, *name, []byte(*input))
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
	case "sign":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		switch *browserName {
		case "firefox":
			if *targetID != "" {
				return errors.New("Firefox signing does not select a browser profile; use --browser firefox")
			}
			result, err = browserextension.SignFirefoxXPI(ctx, *source, *output, *revision)
		case "chrome", "chromium", "edge", "":
			target, resolveErr := resolveSigningTarget(*targetID, *browserName, *executable)
			if resolveErr != nil {
				return resolveErr
			}
			result, err = browserextension.SignChromiumCRX(ctx, target.Browser, target.ExecutablePath, *source, *output, *revision, *keyPath)
		default:
			return errors.New("sign supports chrome, chromium, edge, or firefox; use build-safari for Safari")
		}
	case "package-safari":
		result, err = browserextension.PackageSafariProject(context.Background(), *source, *output, *revision, *bundleID, *appName)
	case "build-safari":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		result, err = browserextension.BuildSafariApp(ctx, *project, *scheme, *output, *teamID)
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
		if *executable != "" || *profile != "" || *profileDirectory != "" {
			return errors.New("external store requests select a browser installation, not an executable or profile")
		}
		selectedBrowser, selectErr := resolveStoreBrowser(*targetID, *browserName)
		if selectErr != nil {
			return selectErr
		}
		result, err = browserextension.RequestStoreInstall(selectedBrowser, *store, *id, *externalDirectory)
	case "remove-store":
		if *executable != "" || *profile != "" || *profileDirectory != "" {
			return errors.New("external store requests select a browser installation, not an executable or profile")
		}
		selectedBrowser, selectErr := resolveStoreBrowser(*targetID, *browserName)
		if selectErr != nil {
			return selectErr
		}
		result, err = browserextension.RemoveStoreInstall(selectedBrowser, *store, *id, *externalDirectory)
	case "policy":
		if *executable != "" || *profile != "" || *profileDirectory != "" || *externalDirectory != "" {
			return errors.New("managed policy selects a browser installation; do not pass executable, profile, or external-dir")
		}
		selectedBrowser, selectErr := resolveStoreBrowser(*targetID, *browserName)
		if selectErr != nil {
			return selectErr
		}
		result, err = browserextension.ManageExtensionPolicy(selectedBrowser, *store, *id, *policyMode)
	default:
		return errors.New("extension requires act, capabilities, prepare, inspect, package, sign, package-safari, build-safari, install, run, stage, install-store, remove-store, or policy")
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

func resolveSigningTarget(id, browser, executable string) (browserextension.DevToolsTarget, error) {
	if id != "" {
		return resolveCLITarget(id, browser, executable, "", "")
	}
	if browser == "" {
		return browserextension.DevToolsTarget{}, errors.New("select --target or --browser for CRX signing")
	}
	if executable != "" {
		return browserextension.DevToolsTarget{Browser: browser, ExecutablePath: executable}, nil
	}
	targets, err := browserextension.DiscoverTargets()
	if err != nil {
		return browserextension.DevToolsTarget{}, err
	}
	var selected string
	for _, target := range targets {
		if target.Browser != browser {
			continue
		}
		if selected != "" && selected != target.ExecutablePath {
			return browserextension.DevToolsTarget{}, errors.New("multiple browser executables found; supply --target or --browser-bin")
		}
		selected = target.ExecutablePath
	}
	if selected == "" {
		return browserextension.DevToolsTarget{}, errors.New("browser executable not discovered; supply --browser-bin")
	}
	return browserextension.DevToolsTarget{Browser: browser, ExecutablePath: selected}, nil
}

func resolveStoreBrowser(id, browser string) (string, error) {
	if id == "" {
		if browser == "" {
			return "", errors.New("select --browser or --target for an external store request")
		}
		return browser, nil
	}
	if browser != "" {
		return "", errors.New("--target cannot be combined with --browser")
	}
	selected, err := browserextension.ResolveTarget(id)
	if err != nil {
		return "", err
	}
	return selected.Browser, nil
}

func resolveStoreActionBrowser(input extensionActionInput) (string, error) {
	if input.Target.ExecutablePath != "" || input.Target.ProfilePath != "" || input.Target.ProfileDirectory != "" {
		return "", errors.New("external requests and policies select a browser installation, not an executable or profile")
	}
	return resolveStoreBrowser(input.Target.ID, input.Target.Browser)
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
	Store             string `json:"store"`
	Mode              string `json:"mode"`
	ExternalDirectory string `json:"externalDirectory"`
	BundleID          string `json:"bundleId"`
	AppName           string `json:"appName"`
	Project           string `json:"project"`
	Scheme            string `json:"scheme"`
	TeamID            string `json:"teamId"`
	KeyPath           string `json:"keyPath"`
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
	return extensionActionContext(context.Background(), name, raw)
}

func extensionActionContext(ctx context.Context, name string, raw []byte) (any, error) {
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
	case "extension.sign":
		if input.Target.Browser == "firefox" {
			return browserextension.SignFirefoxXPI(ctx, input.Source, input.Output, input.Revision)
		}
		target, err := resolveActionSigningTarget(input)
		if err != nil {
			return nil, err
		}
		return browserextension.SignChromiumCRX(ctx, target.Browser, target.ExecutablePath, input.Source, input.Output, input.Revision, input.KeyPath)
	case "extension.package-safari":
		return browserextension.PackageSafariProject(context.Background(), input.Source, input.Output, input.Revision, input.BundleID, input.AppName)
	case "extension.build-safari":
		return browserextension.BuildSafariApp(ctx, input.Project, input.Scheme, input.Output, input.TeamID)
	case "extension.install":
		return nil, errors.New("extension.install is long-running; invoke it through the cymonkey extension act command")
	case "extension.stage":
		target, err := resolveActionTarget(input)
		if err != nil {
			return nil, err
		}
		return browserextension.StageForChromiumWithRevision(target.Browser, input.Source, input.Destination, input.Revision)
	case "extension.install-store":
		browser, err := resolveStoreActionBrowser(input)
		if err != nil {
			return nil, err
		}
		return browserextension.RequestStoreInstall(browser, input.Store, input.ID, input.ExternalDirectory)
	case "extension.remove-store":
		browser, err := resolveStoreActionBrowser(input)
		if err != nil {
			return nil, err
		}
		return browserextension.RemoveStoreInstall(browser, input.Store, input.ID, input.ExternalDirectory)
	case "extension.policy":
		if input.ExternalDirectory != "" {
			return nil, errors.New("managed policy does not use an external extensions directory")
		}
		browser, err := resolveStoreActionBrowser(input)
		if err != nil {
			return nil, err
		}
		return browserextension.ManageExtensionPolicy(browser, input.Store, input.ID, input.Mode)
	default:
		return nil, errors.New("unsupported extension manager action")
	}
}

func resolveActionSigningTarget(input extensionActionInput) (browserextension.DevToolsTarget, error) {
	if input.Target.ProfilePath != "" || input.Target.ProfileDirectory != "" {
		return browserextension.DevToolsTarget{}, errors.New("CRX signing selects a browser executable, not a profile")
	}
	return resolveSigningTarget(input.Target.ID, input.Target.Browser, input.Target.ExecutablePath)
}
