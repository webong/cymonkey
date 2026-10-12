package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"cymonkey/src/internal/browsermanage"
)

// browserHost opens the ext adapter store that holds the browser adapters.
// CYMONKEY_ADAPTER_HOME overrides ext's shared adapter directory, so a trusted
// adapter serves both Cymonkey and ext's own tools by default.
func browserHost() *browsermanage.Host {
	return browsermanage.New(os.Getenv("CYMONKEY_ADAPTER_HOME"))
}

type adapterSummary struct {
	Name              string   `json:"name"`
	Trusted           bool     `json:"trusted"`
	BrowserManagement []string `json:"browserManagement,omitempty"`
}

// adaptersCommand installs, trusts, lists, and removes the ext adapters that
// run browser-specific work. Trust is always an explicit step: installing an
// adapter never lets it run.
func adaptersCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("adapters requires list, install, trust, untrust, or remove")
	}
	host := browserHost()
	store := host.Store()
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return errors.New("adapters list accepts no arguments")
		}
		items, err := store.List()
		if err != nil {
			return err
		}
		summaries := make([]adapterSummary, 0, len(items))
		for _, item := range items {
			trusted, err := store.IsTrusted(item)
			if err != nil {
				return err
			}
			summaries = append(summaries, adapterSummary{Name: item.Manifest.Name, Trusted: trusted, BrowserManagement: item.Manifest.BrowserManagement})
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"adapters": summaries})
	case "install":
		flags := flag.NewFlagSet("adapters install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		source := flags.String("source", "", "adapter directory containing adapter.toml")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*source) == "" {
			return errors.New("adapters install requires --source")
		}
		installed, err := store.Install(*source)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "installed adapter %s; it will not run until you trust it with: cymy adapters trust %s\n", installed.Manifest.Name, installed.Manifest.Name)
		return err
	case "trust", "untrust", "remove":
		if len(args) != 2 {
			return fmt.Errorf("adapters %s requires one adapter name", args[0])
		}
		name := args[1]
		switch args[0] {
		case "trust":
			loaded, err := store.Load(name)
			if err != nil {
				return err
			}
			if err := store.Trust(loaded); err != nil {
				return err
			}
		case "untrust":
			if err := store.RemoveTrust(name); err != nil {
				return err
			}
		default:
			if err := store.Remove(name); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(stdout, "%s adapter %s\n", args[0], name)
		return err
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stderr, "Usage: cymy adapters <list|install|trust|untrust|remove>")
		return err
	default:
		return fmt.Errorf("unknown adapters command %q", args[0])
	}
}
