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

	"cymonkey/src/internal/browsermanage"
)

const extensionUsage = "extension requires act, capabilities, prepare, inspect, package, sign, package-safari, build-safari, install, run, stage, install-store, remove-store, or policy"

// extensionCommand is Cymonkey's manager entry point for extensions supplied
// by external callers. Trusted ext browser adapters implement the
// browser-specific operations; this command selects an adapter and runs the
// management protocol against it (see cymy adapters).
func extensionCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(extensionUsage)
	}
	flags := flag.NewFlagSet("cymonkey extension "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "extension directory, ZIP, signed XPI, or Safari app")
	output := flags.String("output", "", "output ZIP or Safari project location")
	destination := flags.String("destination", "", "staged extension directory")
	browserName := flags.String("browser", "", "browser adapter name, such as chrome, edge, firefox, or safari")
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
	id := flags.String("id", "", "published extension ID")
	store := flags.String("store", "", "store: chrome or edge (defaults to the selected browser)")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var request extensionActionInput
	request.Source, request.Output, request.Destination, request.Revision = *source, *output, *destination, *revision
	request.ID, request.Store, request.Mode, request.ExternalDirectory = *id, *store, *policyMode, *externalDirectory
	request.BundleID, request.AppName, request.Project, request.Scheme, request.TeamID, request.KeyPath = *bundleID, *appName, *project, *scheme, *teamID, *keyPath
	request.Target.ID, request.Target.Browser, request.Target.ExecutablePath = *targetID, *browserName, *executable
	request.Target.ProfilePath, request.Target.ProfileDirectory = *profile, *profileDirectory
	var action string
	switch args[0] {
	case "act":
		parsed, err := parseExtensionActionInput([]byte(*input))
		if err != nil {
			return err
		}
		return runExtensionAction(ctx, *name, parsed, stdout)
	case "prepare", "inspect":
		action = "extension.prepare"
	case "capabilities", "package", "sign", "package-safari", "build-safari", "stage", "install-store", "remove-store", "policy":
		action = "extension." + args[0]
	case "install":
		action = "extension.install"
	case "run":
		action = "extension.run"
	default:
		return errors.New(extensionUsage)
	}
	return runExtensionAction(ctx, action, request, stdout)
}

// runExtensionAction runs one action, writing each progress value and then the
// final result to output as JSON lines.
func runExtensionAction(ctx context.Context, name string, input extensionActionInput, output io.Writer) error {
	encoder := json.NewEncoder(output)
	var last []byte
	result, err := extensionActionContext(ctx, name, input, func(raw json.RawMessage) error {
		last = append(last[:0], raw...)
		return encoder.Encode(raw)
	})
	if err != nil {
		return err
	}
	// An adapter often ends with the same value it last reported as progress.
	if last != nil && bytes.Equal(bytes.TrimSpace(last), bytes.TrimSpace(result)) {
		return nil
	}
	return encoder.Encode(result)
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

// extensionSpec describes how a Cymonkey action name maps onto the
// management protocol.
type extensionSpec struct {
	action  string // protocol action
	adapter string // adapter that owns the action when the caller names none
	profile bool   // the action runs against a browser profile
	forbid  bool   // the action selects a browser installation only
}

var extensionSpecs = map[string]extensionSpec{
	"extension.targets":        {action: "targets"},
	"extension.capabilities":   {action: "capabilities", profile: true},
	"extension.prepare":        {action: "prepare"},
	"extension.package":        {action: "package"},
	"extension.sign":           {action: "sign"},
	"extension.package-safari": {action: "convert", adapter: "safari"},
	"extension.build-safari":   {action: "sign", adapter: "safari"},
	"extension.install":        {action: "install", profile: true},
	"extension.run":            {action: "activate", profile: true},
	"extension.stage":          {action: "stage", profile: true},
	"extension.install-store":  {action: "store_install", forbid: true},
	"extension.remove-store":   {action: "store_remove", forbid: true},
	"extension.policy":         {action: "policy", forbid: true},
}

// extensionActionContext runs one structured action and returns its JSON
// result. progress may be nil. Long-running actions (install, run) report
// progress until they finish or ctx is cancelled.
func extensionActionContext(ctx context.Context, name string, input extensionActionInput, progress browsermanage.Progress) (json.RawMessage, error) {
	spec, ok := extensionSpecs[name]
	if !ok {
		return nil, errors.New("unsupported extension manager action")
	}
	if name == "extension.targets" {
		targets, err := discoverBrowserTargets(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(targets)
	}
	selector := browserSelector{ID: input.Target.ID, Browser: input.Target.Browser, Executable: input.Target.ExecutablePath,
		ProfilePath: input.Target.ProfilePath, ProfileDirectory: input.Target.ProfileDirectory}
	if spec.forbid && (selector.Executable != "" || selector.ProfilePath != "" || selector.ProfileDirectory != "") {
		return nil, errors.New("this action selects a browser installation, not an executable or profile")
	}
	if name == "extension.policy" && input.ExternalDirectory != "" {
		return nil, errors.New("managed policy does not use an external extensions directory")
	}
	fields := map[string]any{}
	var endpoint browsermanage.Endpoint
	var err error
	switch {
	case selector.ID != "" || spec.profile && selector.Browser != "":
		endpoint, fields, err = selector.endpoint(ctx)
	case spec.forbid && selector.Browser == "":
		endpoint, err = adapterEndpoint(spec.adapter, "extension."+spec.action)
	default:
		browser := selector.Browser
		if browser == "" {
			browser = spec.adapter
		}
		endpoint, err = adapterEndpoint(browser, "extension."+spec.action)
		if selector.Executable != "" {
			fields["executable"] = selector.Executable
		}
	}
	if err != nil {
		return nil, err
	}
	for key, value := range map[string]string{
		"source": input.Source, "output": input.Output, "destination": input.Destination, "revision": input.Revision,
		"id": input.ID, "store": input.Store, "policyAction": input.Mode, "externalDirectory": input.ExternalDirectory,
		"bundleId": input.BundleID, "appName": input.AppName, "project": input.Project, "scheme": input.Scheme,
		"teamId": input.TeamID, "keyPath": input.KeyPath,
	} {
		if value != "" {
			fields[key] = value
		}
	}
	return manageExtension(ctx, endpoint, spec.action, fields, progress)
}
