package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"cymonkey/src/internal/provider"
	"cymonkey/src/internal/userscripts"
	"jangolova/browserextension"
)

type repeatFlag []string

var userscriptSlug = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (values *repeatFlag) String() string { return strings.Join(*values, ",") }
func (values *repeatFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func userscriptCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("userscript requires prepare, install, update, list, describe, enable, disable, or uninstall")
	}
	command := args[0]
	if command == "help" || command == "-h" || command == "--help" {
		_, err := fmt.Fprintln(stdout, "Usage: cymonkey userscript <prepare|install|update|list|describe|enable|disable|uninstall> [flags]\n\nInstall accepts --source, infers userscript metadata and a unique local browser target, and optionally connects with --endpoint PROTOCOL=URL. Use --target to select a profile explicitly. Prepare returns an optional review revision.")
		return err
	}
	defaultStore, err := userscripts.DefaultDirectory()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("cymonkey userscript "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	store := flags.String("store", defaultStore, "userscript store directory")
	target := flags.String("target", "", "stable browser/profile target identifier")
	id := flags.String("id", "", "userscript ID")
	name := flags.String("name", "", "userscript display name")
	source := flags.String("source", "", "local JavaScript file")
	revision := flags.String("revision", "", "sha256 revision returned by prepare")
	endpoint := flags.String("endpoint", "", "optional PROTOCOL=URL browser endpoint; keep this command running to activate scripts")
	var matches, excludes repeatFlag
	flags.Var(&matches, "match", "page URL match pattern; repeatable")
	flags.Var(&excludes, "exclude-match", "page URL exclusion pattern; repeatable")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("userscript commands accept flags only")
	}
	return executeUserscriptAction(userscriptOptions{
		Command: command, Store: *store, Target: *target, ID: *id, Name: *name,
		Source: *source, Revision: *revision, Endpoint: *endpoint,
		Matches: []string(matches), Excludes: []string(excludes),
	}, stdout)
}

type userscriptOptions struct {
	Command  string
	Store    string
	Target   string
	ID       string
	Name     string
	Source   string
	Revision string
	Endpoint string
	Matches  []string
	Excludes []string
}

// executeUserscriptAction is shared by the CLI and the operator HTTP surface.
func executeUserscriptAction(options userscriptOptions, stdout io.Writer) error {
	command := options.Command
	store, target, id, name := &options.Store, &options.Target, &options.ID, &options.Name
	source, revision, endpoint := &options.Source, &options.Revision, &options.Endpoint
	matches, excludes := repeatFlag(options.Matches), repeatFlag(options.Excludes)
	encoder := json.NewEncoder(stdout)
	switch command {
	case "prepare", "install", "update":
		if *source == "" {
			return errors.New("--source is required")
		}
		file, err := os.Open(*source)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		metadata := parseUserscriptMetadata(string(data))
		if *name == "" {
			*name = metadata.name
			if *name == "" {
				*name = strings.TrimSuffix(filepath.Base(*source), filepath.Ext(*source))
			}
		}
		if *id == "" {
			*id = strings.Trim(userscriptSlug.ReplaceAllString(strings.ToLower(*name), "-"), "-.")
		}
		if len(matches) == 0 {
			matches = metadata.matches
		}
		if len(excludes) == 0 {
			excludes = metadata.excludes
		}
		resolvedTarget, err := resolveUserscriptTarget(*target, *endpoint)
		if err != nil {
			return err
		}
		record := userscripts.Record{Target: *target, ID: *id, Name: *name,
			Matches:        []string(matches),
			ExcludeMatches: []string(excludes), Enabled: true, Source: string(data)}
		record.Target = resolvedTarget
		record.Revision = userscripts.Revision(record)
		if err := userscripts.Validate(record); err != nil {
			return err
		}
		if command == "prepare" {
			description := record.Description()
			description.Status = "prepared"
			return encoder.Encode(description)
		}
		if *revision != "" && *revision != record.Revision {
			return errors.New("--revision must match the source, target, and permissions returned by userscript prepare")
		}
		if command == "update" {
			previous, err := userscripts.Load(*store, resolvedTarget, *id)
			if err != nil {
				return err
			}
			record.Enabled = previous.Enabled
		}
		if err := userscripts.Save(*store, record, command == "update"); err != nil {
			return err
		}
		description := record.Description()
		if command == "install" {
			description.Status = "stored"
		} else {
			description.Status = "updated"
		}
		if err := encoder.Encode(description); err != nil {
			return err
		}
		if *endpoint != "" {
			return provider.ConnectEngine([]string{"--target-kind", "browser", "--endpoint", *endpoint, "--userscripts-target", resolvedTarget, "--userscripts-store", *store}, stdout, engineRegistry)
		}
		return nil
	case "list":
		if *endpoint != "" {
			return errors.New("--endpoint is supported by install and update only")
		}
		resolvedTarget, err := resolveUserscriptTarget(*target, "")
		if err != nil {
			return err
		}
		records, err := userscripts.List(*store, resolvedTarget)
		if err != nil {
			return err
		}
		result := make([]userscripts.Description, 0, len(records))
		for _, record := range records {
			result = append(result, record.Description())
		}
		return encoder.Encode(result)
	case "describe", "enable", "disable", "uninstall":
		if *endpoint != "" {
			return errors.New("--endpoint is supported by install and update only")
		}
		resolvedTarget, err := resolveUserscriptTarget(*target, "")
		if err != nil {
			return err
		}
		if *id == "" {
			return errors.New("--id is required")
		}
		if command == "uninstall" {
			if err := userscripts.Remove(*store, resolvedTarget, *id); err != nil {
				return err
			}
			return encoder.Encode(map[string]any{"target": resolvedTarget, "id": *id, "uninstalled": true})
		}
		var record userscripts.Record
		if command == "describe" {
			record, err = userscripts.Load(*store, resolvedTarget, *id)
		} else {
			record, err = userscripts.SetEnabled(*store, resolvedTarget, *id, command == "enable")
		}
		if err != nil {
			return err
		}
		description := record.Description()
		if command == "enable" || command == "disable" {
			description.Status = command + "d"
		}
		return encoder.Encode(description)
	default:
		return fmt.Errorf("unknown userscript command %q", command)
	}
}

type userscriptSourceMetadata struct {
	name     string
	matches  []string
	excludes []string
}

func parseUserscriptMetadata(source string) userscriptSourceMetadata {
	var metadata userscriptSourceMetadata
	inside := false
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "// ==UserScript==" {
			inside = true
			continue
		}
		if line == "// ==/UserScript==" {
			break
		}
		if !inside {
			continue
		}
		if value, ok := strings.CutPrefix(line, "// @name "); ok {
			metadata.name = strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(line, "// @match "); ok {
			metadata.matches = append(metadata.matches, strings.TrimSpace(value))
		}
		if value, ok := strings.CutPrefix(line, "// @exclude-match "); ok {
			metadata.excludes = append(metadata.excludes, strings.TrimSpace(value))
		}
	}
	return metadata
}

func resolveUserscriptTarget(explicit, endpoint string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	if endpoint != "" {
		if _, address, ok := strings.Cut(endpoint, "="); !ok || strings.TrimSpace(address) == "" {
			return "", errors.New("--endpoint must use PROTOCOL=URL")
		}
		digest := sha256.Sum256([]byte(endpoint))
		return "endpoint:" + hex.EncodeToString(digest[:12]), nil
	}
	targets, err := browserextension.DiscoverTargets()
	if err != nil {
		return "", err
	}
	compatible := make([]browserextension.BrowserTarget, 0, len(targets))
	for _, target := range targets {
		if target.Browser != "safari" {
			compatible = append(compatible, target)
		}
	}
	if len(compatible) == 1 {
		return compatible[0].ID, nil
	}
	if len(compatible) == 0 {
		return "", errors.New("no local browser profile found; pass --target with the browser/profile ID")
	}
	return "", errors.New("multiple browser profiles found; select --target from cmy browser targets")
}
