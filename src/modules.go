package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	jangolovaregistry "cymonkey/lib/jangolova/registry"
)

func modulesCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("modules requires discover or pull")
	}
	switch args[0] {
	case "discover":
		return discoverModules(args[1:], stdout)
	case "pull":
		return pullModule(args[1:], stdout)
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stderr, "Usage: cymonkey modules <discover|pull> [flags]")
		return err
	default:
		return fmt.Errorf("unknown modules command %q", args[0])
	}
}

func discoverModules(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("cymonkey modules discover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	endpoint := flags.String("registry", "", "Jangolova registry HTTPS endpoint")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*endpoint) == "" {
		return errors.New("modules discover requires --registry and no positional arguments")
	}
	registry, err := jangolovaregistry.Discover(context.Background(), *endpoint, jangolovaregistry.DiscoveryOptions{})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(registry)
}

func pullModule(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("cymonkey modules pull", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	endpoint := flags.String("registry", "", "Jangolova registry HTTPS endpoint")
	moduleID := flags.String("module", "", "canonical Jangolova module ID")
	platform := flags.String("platform", "", "target platform, for example linux-amd64")
	cache := flags.String("cache", "", "explicit module cache directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*endpoint) == "" || strings.TrimSpace(*moduleID) == "" || strings.TrimSpace(*platform) == "" || strings.TrimSpace(*cache) == "" {
		return errors.New("modules pull requires --registry, --module, --platform, --cache and no positional arguments")
	}
	registry, err := jangolovaregistry.Discover(context.Background(), *endpoint, jangolovaregistry.DiscoveryOptions{})
	if err != nil {
		return err
	}
	module, err := registry.Select(*moduleID, *platform)
	if err != nil {
		return err
	}
	path, err := jangolovaregistry.Pull(context.Background(), module, *platform, *cache, jangolovaregistry.PullOptions{})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]string{"module": module.ID, "version": module.Version, "path": path})
}
