package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"blockade"
	blockadeplugin "blockade/plugin"
	"board"
	boardplugin "board/plugin"
	"cymonkey/src/internal/orchestrator"
	jangolovaplugin "jangolova/plugin"
	"providerplugin"
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

func registerJangolovaPlugins(registry *orchestrator.Registry) error {
	installed, err := installedPlugins()
	if err != nil {
		return err
	}
	for _, item := range jangolovaplugin.Adapters(installed) {
		if err := registry.RegisterEngine(item.Name, wrapJangolova(item.Adapter)); err != nil {
			return err
		}
	}
	return nil
}
func registerBlockadePlugins(registry *blockade.ProviderAdapterRegistry) error {
	installed, err := installedPlugins()
	if err != nil {
		return err
	}
	return blockadeplugin.RegisterInstalled(registry, installed)
}
func boardPluginProviders() ([]board.Provider, error) {
	installed, err := installedPlugins()
	if err != nil {
		return nil, err
	}
	return boardplugin.Providers(installed), nil
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
		_, err := fmt.Fprintln(stderr, "Usage: cymonkey plugins <install --manifest PATH|upgrade --manifest PATH|list|remove NAME>")
		return err
	default:
		return fmt.Errorf("unknown plugins command %q", args[0])
	}
}
