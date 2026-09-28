package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	root, err := providerplugin.DefaultRoot()
	if err != nil {
		return nil, err
	}
	return providerplugin.List(root)
}

func registerJangolovaPlugins(registry *orchestrator.Registry) error {
	installed, err := installedPlugins()
	if err != nil {
		return err
	}
	for _, item := range installed {
		if item.Manifest.Kind == providerplugin.JangolovaEngine {
			if err := registry.RegisterEngine(item.Manifest.Name, wrapJangolova(jangolovaplugin.Adapter{Installed: item})); err != nil {
				return err
			}
		}
	}
	return nil
}
func registerBlockadePlugins(registry *blockade.ProviderAdapterRegistry) error {
	installed, err := installedPlugins()
	if err != nil {
		return err
	}
	for _, item := range installed {
		if item.Manifest.Kind == providerplugin.BlockadeProvider {
			if err := blockadeplugin.Register(registry, item); err != nil {
				return err
			}
		}
	}
	return nil
}
func boardPluginProviders() ([]board.Provider, error) {
	installed, err := installedPlugins()
	if err != nil {
		return nil, err
	}
	var providers []board.Provider
	for _, item := range installed {
		if item.Manifest.Kind == providerplugin.BoardProvider {
			providers = append(providers, boardplugin.Provider{Installed: item})
		}
	}
	return providers, nil
}

func pluginsCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("plugins requires install, upgrade, list, or remove")
	}
	root, err := providerplugin.DefaultRoot()
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
