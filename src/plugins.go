package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"kit/contentkit"
	contentplugin "kit/contentkit/plugin"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	boardconfig "board/config"
	"cymonkey/src/internal/displayplugin"
	"cymonkey/src/internal/orchestrator"
	"kit/devicekit"
	deviceplugin "kit/devicekit/plugin"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
)

const (
	displayPluginContract = displayplugin.ContractName
	contentPluginContract = contentplugin.ContractName
	devicePluginContract  = deviceplugin.ContractName
	pluginContractVersion = "v1"
)

// pluginOperations maps each public contract to the operations its libraries
// serve, which ext requires every package to declare.
var pluginOperations = map[string][]string{
	displayPluginContract: displayplugin.Operations,
	contentPluginContract: contentplugin.Operations,
	devicePluginContract:  deviceplugin.Operations,
}

// pluginListTTL bounds how long a verified plugin list is reused. ext's store
// verifies every installed package's content whenever it lists them, which
// costs seconds for a large vendored tree. One command lists the plugins for
// display, device and content setup within moments of each other, so a short
// reuse window avoids repeating that work without hiding a change for long.
// Launching a plugin still re-verifies it.
const pluginListTTL = 10 * time.Second

var pluginList struct {
	sync.Mutex
	root    string
	loaded  time.Time
	plugins []store.Installed
}

func installedPlugins() ([]store.Installed, error) {
	root, err := defaultPluginRoot()
	if err != nil {
		return nil, err
	}
	pluginList.Lock()
	defer pluginList.Unlock()
	if pluginList.plugins != nil && pluginList.root == root && time.Since(pluginList.loaded) < pluginListTTL {
		return append([]store.Installed(nil), pluginList.plugins...), nil
	}
	packages, err := store.New(root, store.Limits{})
	if err != nil {
		return nil, err
	}
	listed, err := packages.List()
	if err != nil {
		return nil, err
	}
	if listed == nil {
		listed = []store.Installed{}
	}
	pluginList.root, pluginList.loaded, pluginList.plugins = root, time.Now(), listed
	return append([]store.Installed(nil), listed...), nil
}

// forgetInstalledPlugins drops the reused list after a change to the store.
func forgetInstalledPlugins() {
	pluginList.Lock()
	pluginList.plugins = nil
	pluginList.Unlock()
}

// pluginContract declares a Cymonkey contract with the operations its
// libraries serve, which ext requires every package to list.
func pluginContract(name string) plugin.Contract {
	operations := make([]plugin.Operation, 0, len(pluginOperations[name]))
	for _, operation := range pluginOperations[name] {
		operations = append(operations, plugin.Operation{Name: operation})
	}
	return plugin.Contract{ContractRef: plugin.ContractRef{Name: name, Version: pluginContractVersion}, Operations: operations}
}

// packageDirectory returns the directory holding package.json for a path that
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
		if err := registry.RegisterEngine(item.Name(), displayplugin.Adapter{Installed: item}); err != nil {
			return err
		}
	}
	return nil
}
func devicePluginAdapters() ([]devicekit.Provider, error) {
	installed, err := installedPlugins()
	if err != nil {
		return nil, err
	}
	providers := make([]devicekit.Provider, 0, len(installed))
	for _, item := range installed {
		if item.Supports(devicePluginContract, pluginContractVersion) {
			providers = append(providers, deviceplugin.Provider{Installed: item})
		}
	}
	if path := os.Getenv("CYMONKEY_DEVICE_CONFIG"); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		configured, err := boardconfig.Load(file)
		if err != nil {
			return nil, err
		}
		providers = append(providers, configured...)
	}
	return providers, nil
}

func contentPluginRegistry() (*contentkit.ProviderAdapterRegistry, error) {
	installed, err := installedPlugins()
	if err != nil {
		return nil, err
	}
	registry := contentkit.NewProviderAdapterRegistry()
	runtimeRoot, err := defaultContentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	for _, item := range installed {
		if !item.Supports(contentPluginContract, pluginContractVersion) {
			continue
		}
		if err := contentplugin.RegisterWithRuntimeRoot(registry, item, runtimeRoot); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func pluginsCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("plugins requires install, upgrade, list, or remove")
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
		pluginType := flags.String("type", "", "display, content, or device")
		name := flags.String("name", "", "plugin package name")
		version := flags.String("version", "", "plugin semantic version")
		command := flags.String("command", "", "plugin executable path (executable package)")
		root := flags.String("root", "", "directory holding the package files (source package)")
		var files, directories stringList
		flags.Var(&files, "file", "file within --root to include; repeatable (source package)")
		flags.Var(&directories, "dir", "directory within --root to include recursively; repeatable (source package)")
		stage := flags.String("stage", "", "copy the listed files into this empty directory and write package.json there (source package)")
		output := flags.String("output", "", "package.json output path; defaults beside the executable or in --root")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		contract, ok := map[string]string{
			"display": displayPluginContract,
			"content": contentPluginContract,
			"device":  devicePluginContract,
		}[strings.TrimSpace(*pluginType)]
		source := strings.TrimSpace(*root) != ""
		if flags.NArg() != 0 || !ok || strings.TrimSpace(*name) == "" || strings.TrimSpace(*version) == "" || (strings.TrimSpace(*command) == "") == !source {
			return errors.New("plugins manifest requires --type display|content|device, --name, --version, and either --command or --root with --file/--dir")
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
		manifest := flags.String("manifest", "", "local package.json path or package directory")
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
		forgetInstalledPlugins()
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(item.Manifest)
	case "upgrade":
		flags := flag.NewFlagSet("plugins upgrade", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifest := flags.String("manifest", "", "local package.json path or package directory")
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
		forgetInstalledPlugins()
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(item.Manifest)
	case "list":
		if len(args) != 1 {
			return errors.New("plugins list accepts no arguments")
		}
		items, err := packages.List()
		if err != nil {
			return err
		}
		manifests := make([]packagekit.Manifest, 0, len(items))
		for _, item := range items {
			manifests = append(manifests, item.Manifest)
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"plugins": manifests})
	case "remove":
		if len(args) != 2 {
			return errors.New("plugins remove requires one name")
		}
		forgetInstalledPlugins()
		if err := packages.Remove(args[1]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "removed plugin %s\n", args[1])
		return err
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stderr, "Usage: cymy plugins <manifest|install|upgrade|list|remove>")
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
