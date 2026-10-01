package main

import (
	"blockade"
	blockadeplugin "blockade/plugin"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"board"
	boardconfig "board/config"
	boardplugin "board/plugin"
	"cymonkey/src/contracts"
	"cymonkey/src/internal/orchestrator"
	displayplugin "jangolova/plugin"
	"providerplugin"
)

const (
	displayPluginContract = "cymonkey.display"
	contentPluginContract = "cymonkey.content"
	devicePluginContract  = "cymonkey.device"
	pluginContractVersion = "v1alpha1"
)

func installedPlugins() ([]providerplugin.Installed, error) {
	root, err := defaultPluginRoot()
	if err != nil {
		return nil, err
	}
	return providerplugin.List(root)
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
		if !item.Manifest.Supports(displayPluginContract, pluginContractVersion) {
			continue
		}
		if err := registry.RegisterEngine(item.Manifest.Name, wrapDisplayLibrary(displayplugin.Adapter{Installed: item, MethodPrefix: displayPluginContract})); err != nil {
			return err
		}
	}
	return nil
}
func devicePluginAdapters() ([]board.Provider, error) {
	installed, err := installedPlugins()
	if err != nil {
		return nil, err
	}
	providers := make([]board.Provider, 0, len(installed))
	for _, item := range installed {
		if item.Manifest.Supports(devicePluginContract, pluginContractVersion) {
			providers = append(providers, boardplugin.Provider{Installed: item, MethodPrefix: devicePluginContract})
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

func contentPluginRegistry() (*blockade.ProviderAdapterRegistry, error) {
	installed, err := installedPlugins()
	if err != nil {
		return nil, err
	}
	registry := blockade.NewProviderAdapterRegistry()
	runtimeRoot, err := defaultContentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	for _, item := range installed {
		if !item.Manifest.Supports(contentPluginContract, pluginContractVersion) {
			continue
		}
		if err := blockadeplugin.RegisterWithProtocol(registry, item, blockadeplugin.Protocol{
			MethodPrefix: contentPluginContract, AdapterAPIVersion: contracts.ContentAdapterVersion,
			ObservationRequestAPI: contracts.ObservationRequestVersion, ObservationAPIVersion: contracts.ObservationAPIVersion,
			ContentAPIVersion: contracts.ContentAPIVersion,
			ModelAPIVersion:   contracts.ModelManifestVersion, ManifestEnvironment: "CYMONKEY_PLUGIN_MANIFEST",
			RuntimeRoot: runtimeRoot, DescriptorFile: "content.json", DescriptorAPIVersion: contracts.ContentSourceVersion,
		}); err != nil {
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
	switch args[0] {
	case "manifest":
		flags := flag.NewFlagSet("plugins manifest", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		pluginType := flags.String("type", "", "display, content, or device")
		name := flags.String("name", "", "plugin package name")
		version := flags.String("version", "", "plugin semantic version")
		command := flags.String("command", "", "plugin executable path")
		output := flags.String("output", "", "plugin.json output path; defaults beside the executable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		contract, ok := map[string]string{
			"display": displayPluginContract,
			"content": contentPluginContract,
			"device":  devicePluginContract,
		}[strings.TrimSpace(*pluginType)]
		if flags.NArg() != 0 || !ok || strings.TrimSpace(*name) == "" || strings.TrimSpace(*version) == "" || strings.TrimSpace(*command) == "" {
			return errors.New("plugins manifest requires --type display|content|device, --name, --version, and --command")
		}
		absCommand, err := filepath.Abs(*command)
		if err != nil {
			return err
		}
		file, err := os.Open(absCommand)
		if err != nil {
			return err
		}
		hash := sha256.New()
		n, copyErr := io.Copy(hash, io.LimitReader(file, (128<<20)+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		if n > 128<<20 {
			return errors.New("plugin command exceeds 128 MiB")
		}
		outputPath := strings.TrimSpace(*output)
		if outputPath == "" {
			outputPath = filepath.Join(filepath.Dir(absCommand), "plugin.json")
		}
		absOutput, err := filepath.Abs(outputPath)
		if err != nil {
			return err
		}
		if filepath.Dir(absOutput) != filepath.Dir(absCommand) {
			return errors.New("plugin.json must be written beside the plugin command")
		}
		manifest := providerplugin.Manifest{
			APIVersion: contracts.PluginManifestVersion,
			Name:       strings.TrimSpace(*name),
			Version:    strings.TrimSpace(*version),
			Contracts:  []providerplugin.Contract{{Name: contract, Version: pluginContractVersion}},
			Platform:   runtime.GOOS + "-" + runtime.GOARCH,
			Command:    filepath.Base(absCommand),
			SHA256:     hex.EncodeToString(hash.Sum(nil)),
		}
		if err := manifest.Validate(); err != nil {
			return err
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
		manifest := flags.String("manifest", "", "local plugin.json path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifest) == "" {
			return errors.New("plugins install requires --manifest")
		}
		item, err := providerplugin.Install(root, *manifest)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(item.Manifest)
	case "upgrade":
		flags := flag.NewFlagSet("plugins upgrade", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifest := flags.String("manifest", "", "local plugin.json path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifest) == "" {
			return errors.New("plugins upgrade requires --manifest")
		}
		item, err := providerplugin.Upgrade(root, *manifest)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(item.Manifest)
	case "list":
		if len(args) != 1 {
			return errors.New("plugins list accepts no arguments")
		}
		items, err := providerplugin.List(root)
		if err != nil {
			return err
		}
		manifests := make([]providerplugin.Manifest, 0, len(items))
		for _, item := range items {
			manifests = append(manifests, item.Manifest)
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"plugins": manifests})
	case "remove":
		if len(args) != 2 {
			return errors.New("plugins remove requires one name")
		}
		if err := providerplugin.Remove(root, args[1]); err != nil {
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
