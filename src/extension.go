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
		return errors.New("extension requires act, capabilities, prepare, inspect, package, run, stage, or install-store")
	}
	flags := flag.NewFlagSet("cymonkey extension "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "extension directory or ZIP")
	output := flags.String("output", "", "output ZIP path")
	destination := flags.String("destination", "", "staged extension directory")
	browserName := flags.String("browser", "", "selected browser: chrome, chromium, or edge")
	revision := flags.String("revision", "", "revision returned by extension inspect")
	executable := flags.String("browser-bin", "", "absolute path to the selected browser executable")
	profile := flags.String("profile", "", "absolute path to the selected browser profile")
	headless := flags.Bool("headless", true, "run the install transaction without a browser window")
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
		result, err = extensionAction(*name, []byte(*input))
	case "run":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return browserextension.RunWithDevTools(ctx, browserextension.DevToolsTarget{
			Browser: *browserName, ExecutablePath: *executable, ProfilePath: *profile, Headless: *headless,
		}, *source, *destination, *revision, func(result browserextension.InstallResult) error {
			return json.NewEncoder(stdout).Encode(result)
		})
	case "capabilities":
		result = browserextension.Capability(*browserName)
	case "prepare", "inspect":
		result, err = browserextension.Inspect(*source)
	case "package":
		result, err = browserextension.Package(*source, *output)
	case "install":
		return errors.New("persistent local extension installation is unavailable for this target; use extension run for a managed browser session")
	case "stage":
		result, err = browserextension.StageForChromiumWithRevision(*browserName, *source, *destination, *revision)
	case "install-store":
		result, err = browserextension.RequestChromeWebStoreInstall(*id, *externalDirectory)
	default:
		return errors.New("extension requires act, capabilities, prepare, inspect, package, run, stage, or install-store")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

// extensionAction is the structured manager contract used by other local
// tools. It accepts caller-supplied packages; it never assumes ownership of
// their source or grants browser installation without the browser's consent.
func extensionAction(name string, raw []byte) (any, error) {
	var input struct {
		Source            string `json:"source"`
		Output            string `json:"output"`
		Destination       string `json:"destination"`
		Revision          string `json:"revision"`
		ID                string `json:"id"`
		ExternalDirectory string `json:"externalDirectory"`
		Target            struct {
			Browser string `json:"browser"`
		} `json:"target"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("extension action input must be one JSON object")
	}
	switch name {
	case "extension.capabilities":
		return browserextension.Capability(input.Target.Browser), nil
	case "extension.prepare":
		return browserextension.Inspect(input.Source)
	case "extension.package":
		return browserextension.Package(input.Source, input.Output)
	case "extension.install":
		return nil, errors.New("persistent local extension installation is unavailable for this target; use the long-running extension run command for a managed browser session")
	case "extension.stage":
		return browserextension.StageForChromiumWithRevision(input.Target.Browser, input.Source, input.Destination, input.Revision)
	case "extension.install-store":
		return browserextension.RequestChromeWebStoreInstall(input.ID, input.ExternalDirectory)
	default:
		return nil, errors.New("unsupported extension manager action")
	}
}
