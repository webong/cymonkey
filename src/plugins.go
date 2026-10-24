package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	consoleservice "jangolova/consolekit/service"
	"jangolova/contentkit"
	"jangolova/contentkit/service"
	"jangolova/displaykit"
	gatewayservice "jangolova/gatewaykit/service"
	"jangolova/mobilekit"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"cymonkey/src/internal/displayplugin"
	"cymonkey/src/internal/orchestrator"
	"jangolova/devicekit"
	deviceplugin "jangolova/devicekit/plugin"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
)

const (
	displayPluginContract  = displayplugin.ContractName
	contentPluginContract  = contentkit.ProviderContract
	contentServiceContract = service.ContractName
	consoleServiceContract = consoleservice.ContractName
	gatewayServiceContract = gatewayservice.ContractName
	devicePluginContract   = deviceplugin.ContractName
	mobileTargetContract   = mobilekit.Contract
	pluginContractVersion  = "v1"
)

// pluginOperations maps each public contract to the operations its libraries
// serve, which ext requires every package to declare.
var pluginOperations = map[string][]string{
	displayPluginContract:  displayplugin.Operations,
	contentPluginContract:  contentkit.ProviderOperations,
	contentServiceContract: service.Operations,
	consoleServiceContract: consoleservice.Operations,
	gatewayServiceContract: gatewayservice.Operations,
	devicePluginContract:   deviceplugin.Operations,
	// A mobile target serves its own start and stop plus the device operations
	// of its viewer half, under one contract.
	mobileTargetContract: append(append([]string(nil), mobilekit.ControlOperations...), deviceplugin.Operations...),
}

// installedPlugins lists the installed packages from their manifests alone.
// Listing does not hash package content, so a large vendored tree costs
// nothing here; a package that cannot be read is left out of discovery and is
// reported by `plugins list`. Nothing is launched from this list unverified:
// opening a plugin re-verifies its content first.
func installedPlugins() ([]store.Installed, error) {
	root, err := defaultPluginRoot()
	if err != nil {
		return nil, err
	}
	packages, err := store.New(root, store.Limits{})
	if err != nil {
		return nil, err
	}
	listed, _, err := packages.ListUnverified()
	if err != nil {
		return nil, err
	}
	if listed == nil {
		listed = []store.Installed{}
	}
	return listed, nil
}

// pluginContract declares a Cymonkey contract with the operations its
// libraries serve, which ext requires every package to list.
func pluginContract(name string) plugin.Contract {
	if name == contentServiceContract {
		return service.Contract() // its command operation reports progress
	}
	if name == consoleServiceContract {
		return consoleservice.Contract()
	}
	if name == gatewayServiceContract {
		return gatewayservice.Contract()
	}
	operations := make([]plugin.Operation, 0, len(pluginOperations[name]))
	for _, operation := range pluginOperations[name] {
		operations = append(operations, plugin.Operation{Name: operation})
	}
	return plugin.Contract{ContractRef: plugin.ContractRef{Name: name, Version: pluginContractVersion}, Operations: operations}
}

// packageDirectory returns the directory holding plugin.json for a path that
// names either that directory or the manifest file.
func packageDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return absolute, nil
	}
	if filepath.Base(absolute) != store.ManifestName {
		return "", errors.New("plugin manifest must be named " + store.ManifestName)
	}
	return filepath.Dir(absolute), nil
}

func defaultPluginRoot() (string, error) {
	if value := os.Getenv("CYMONKEY_PLUGIN_DIR"); value != "" {
		return filepath.Abs(value)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cymonkey", "plugins"), nil
}

func registerDisplayPlugins(registry *orchestrator.Registry) error {
	installed, err := installedPlugins()
	if err != nil {
		return err
	}
	for _, item := range installed {
		if !item.Supports(displayPluginContract, pluginContractVersion) {
			continue
		}
		if err := registry.RegisterEngine(item.Name(), displayplugin.Adapter{Installed: item, Policy: displayPolicy{}, Environment: displayLaunchEnvironment()}); err != nil {
			return err
		}
	}
	return nil
}

// devicePluginAdapters returns one provider per provider ID that each installed
// device.provider package reports. CYMONKEY_DEVICE_CONFIG names a device
// inventory file; it is handed to the packages as a launch setting, and the
// packages build their own providers from it.
func devicePluginAdapters() ([]devicekit.Provider, error) {
	installed, err := installedPlugins()
	if err != nil {
		return nil, err
	}
	settings := map[string]string{}
	if path := os.Getenv("CYMONKEY_DEVICE_CONFIG"); path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		settings["inventory"] = absolute
	}
	providers := make([]devicekit.Provider, 0, len(installed))
	for _, item := range installed {
		if !item.Supports(devicePluginContract, pluginContractVersion) {
			continue
		}
		discovered, err := deviceplugin.Discover(context.Background(), item, settings)
		if err != nil {
			// A package that cannot start still appears under its own name, so
			// listing it reports the failure instead of hiding the package.
			providers = append(providers, deviceplugin.Provider{Installed: item, Settings: settings})
			continue
		}
		for _, provider := range discovered {
			providers = append(providers, provider)
		}
	}
	return providers, nil
}

// keyboardPluginProvider returns the macOS keyboard provider bound to pid from
// whichever installed device.provider package serves it.
func keyboardPluginProvider(pid int) (devicekit.Provider, error) {
	installed, err := installedPlugins()
	if err != nil {
		return nil, err
	}
	settings := map[string]string{"keyboardPid": strconv.Itoa(pid)}
	var failure error
	for _, item := range installed {
		if !item.Supports(devicePluginContract, pluginContractVersion) {
			continue
		}
		discovered, err := deviceplugin.Discover(context.Background(), item, settings)
		if err != nil {
			failure = err
			continue
		}
		for _, provider := range discovered {
			if provider.ID() == "macos-keyboard" {
				return provider, nil
			}
		}
	}
	if failure != nil {
		return nil, failure
	}
	return nil, errors.New("no installed device plugin serves the macOS keyboard; install the board provider (pkg/board/cmd/provider)")
}

func pluginsCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("plugins requires install, upgrade, list, check, or remove")
	}
	root, err := defaultPluginRoot()
	if err != nil {
		return err
	}
	packages, err := store.New(root, store.Limits{})
	if err != nil {
		return err
	}
	switch args[0] {
	case "manifest":
		flags := flag.NewFlagSet("plugins manifest", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		pluginType := flags.String("type", "", "display, content, content-service, console-service, gateway-service, device, or mobile-target")
		name := flags.String("name", "", "plugin package name")
		version := flags.String("version", "", "plugin semantic version")
		command := flags.String("command", "", "plugin executable path (executable package)")
		root := flags.String("root", "", "directory holding the package files (source package)")
		var files, directories stringList
		flags.Var(&files, "file", "file within --root to include; repeatable (source package)")
		flags.Var(&directories, "dir", "directory within --root to include recursively; repeatable (source package)")
		stage := flags.String("stage", "", "copy the listed files into this empty directory and write plugin.json there (source package)")
		output := flags.String("output", "", "plugin.json output path; defaults beside the executable or in --root")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		contract, ok := map[string]string{
			"display":         displayPluginContract,
			"content":         contentPluginContract,
			"content-service": contentServiceContract,
			"console-service": consoleServiceContract,
			"gateway-service": gatewayServiceContract,
			"device":          devicePluginContract,
			"mobile-target":   mobileTargetContract,
		}[strings.TrimSpace(*pluginType)]
		source := strings.TrimSpace(*root) != ""
		if flags.NArg() != 0 || !ok || strings.TrimSpace(*name) == "" || strings.TrimSpace(*version) == "" || (strings.TrimSpace(*command) == "") == !source {
			return errors.New("plugins manifest requires --type display|content|content-service|console-service|gateway-service|device|mobile-target, --name, --version, and either --command or --root with --file/--dir")
		}
		var manifest packagekit.Manifest
		var err error
		var home string
		var included []string
		if source {
			home, err = filepath.Abs(*root)
			if err != nil {
				return err
			}
			entries, copies, listErr := sourcePackageFiles(home, files, directories)
			if listErr != nil {
				return listErr
			}
			included = entries
			if strings.TrimSpace(*stage) != "" {
				// Install refuses a directory holding unlisted files, so staging
				// gives a working tree an exact package directory.
				staged, stageErr := stageSourcePackage(home, strings.TrimSpace(*stage), copies)
				if stageErr != nil {
					return stageErr
				}
				home = staged
			}
			manifest, err = packagekit.SourceManifest(strings.TrimSpace(*name), strings.TrimSpace(*version), []plugin.Contract{pluginContract(contract)}, home, included)
		} else {
			absCommand, absErr := filepath.Abs(*command)
			if absErr != nil {
				return absErr
			}
			home = filepath.Dir(absCommand)
			manifest, err = packagekit.ExecutableManifest(strings.TrimSpace(*name), strings.TrimSpace(*version), []plugin.Contract{pluginContract(contract)}, absCommand)
		}
		if err != nil {
			return err
		}
		if contract == displayPluginContract {
			// A display engine asks the host for endpoint policy, the worker
			// environment and redaction, so its package declares those calls.
			if manifest, err = manifest.WithReverse([]plugin.Contract{displaykit.HostContract()}); err != nil {
				return err
			}
		}
		outputPath := strings.TrimSpace(*output)
		if outputPath == "" {
			outputPath = filepath.Join(home, store.ManifestName)
		}
		absOutput, err := filepath.Abs(outputPath)
		if err != nil {
			return err
		}
		if absOutput != filepath.Join(home, store.ManifestName) {
			return errors.New(store.ManifestName + " must be written in the package directory")
		}
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(absOutput, append(encoded, '\n'), 0o600); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, absOutput)
		return err
	case "install":
		flags := flag.NewFlagSet("plugins install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifest := flags.String("manifest", "", "local plugin.json path or package directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifest) == "" {
			return errors.New("plugins install requires --manifest")
		}
		source, err := packageDirectory(*manifest)
		if err != nil {
			return err
		}
		item, err := packages.Install(source)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(item.Manifest)
	case "upgrade":
		flags := flag.NewFlagSet("plugins upgrade", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifest := flags.String("manifest", "", "local plugin.json path or package directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifest) == "" {
			return errors.New("plugins upgrade requires --manifest")
		}
		source, err := packageDirectory(*manifest)
		if err != nil {
			return err
		}
		item, err := packages.Upgrade(source)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(item.Manifest)
	case "list":
		if len(args) != 1 {
			return errors.New("plugins list accepts no arguments")
		}
		items, problems, err := packages.ListUnverified()
		if err != nil {
			return err
		}
		manifests := make([]packagekit.Manifest, 0, len(items))
		for _, item := range items {
			manifests = append(manifests, item.Manifest)
		}
		unreadable := make([]map[string]string, 0, len(problems))
		for _, problem := range problems {
			unreadable = append(unreadable, map[string]string{"id": problem.ID, "error": problem.Err.Error()})
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"plugins": manifests, "unreadable": unreadable})
	case "check":
		if len(args) != 1 {
			return errors.New("plugins check accepts no arguments")
		}
		items, _, err := packages.ListUnverified()
		if err != nil {
			return err
		}
		checkPlugins(items, stdout)
		return nil
	case "remove":
		if len(args) != 2 {
			return errors.New("plugins remove requires one name")
		}
		if err := packages.Remove(args[1]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "removed plugin %s\n", args[1])
		return err
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stderr, "Usage: cymy plugins <manifest|install|upgrade|list|check|remove>")
		return err
	default:
		return fmt.Errorf("unknown plugins command %q", args[0])
	}
}

// stringList collects a repeatable flag.
type stringList []string

func (list *stringList) String() string     { return strings.Join(*list, ",") }
func (list *stringList) Set(v string) error { *list = append(*list, v); return nil }

// sourcePackageFiles resolves --file and --dir into the package entries and the
// files to copy when staging. Each --dir becomes one tree artifact: it is a
// single manifest entry covered by a directory digest, so a vendored dependency
// tree needs no per-file entry. Python bytecode caches are left out of the copy.
// Paths are slash separated and relative to root.
func sourcePackageFiles(root string, files, directories []string) (entries, copies []string, err error) {
	seen := map[string]bool{}
	copy := func(relative string) {
		relative = filepath.ToSlash(relative)
		if !seen[relative] {
			seen[relative] = true
			copies = append(copies, relative)
		}
	}
	for _, file := range files {
		entries = append(entries, filepath.ToSlash(file))
		copy(file)
	}
	for _, directory := range directories {
		directory = filepath.ToSlash(filepath.Clean(filepath.FromSlash(directory)))
		entries = append(entries, directory)
		start := filepath.Join(root, filepath.FromSlash(directory))
		walkErr := filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("source package file %s is not a regular file", path)
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			copy(relative)
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}
	sort.Strings(entries)
	sort.Strings(copies)
	return entries, copies, nil
}

// stageSourcePackage copies the listed files into an empty directory. Links
// are followed so the staged package holds regular files only, and a file keeps
// its owner-execute bit.
func stageSourcePackage(root, destination string, files []string) (string, error) {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if entries, err := os.ReadDir(destination); err == nil && len(entries) > 0 {
		return "", errors.New("--stage directory must be empty")
	}
	for _, file := range files {
		source := filepath.Join(root, filepath.FromSlash(file))
		info, err := os.Stat(source)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("source package file %s is not a regular file", file)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return "", err
		}
		mode := os.FileMode(0o600)
		if info.Mode().Perm()&0o100 != 0 {
			mode = 0o700
		}
		target := filepath.Join(destination, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return "", err
		}
	}
	return destination, nil
}
