package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	"jangolova/commandkit"
	commandrun "jangolova/commandkit/run"
	consoleservice "jangolova/consolekit/service"
)

// consolePassthroughEnvironment names the host variables the console may need
// to find its home. ext starts a plugin with a scrubbed environment, so nothing
// else reaches it unless the console configuration or a command names it.
var consolePassthroughEnvironment = []string{
	"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME",
	"TMPDIR", "TEMP", "TMP",
}

// consoleCommand runs a `cymy console` command in the installed console service
// package. Commands that speak a stdio protocol of their own (the MCP server and
// the live event stream) run the package's verified executable as a child with
// this process's input and output; every other command is one call whose log
// lines stream back while it runs.
func consoleCommand(args []string, stdout, stderr io.Writer) error {
	installed, err := consoleServicePackage()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	environment := consoleLaunchEnvironment(args)
	if len(args) > 0 && (args[0] == "mcp" || args[0] == "stream") {
		return commandrun.Exec(ctx, installed, args, environment, os.Stdin, stdout, stderr)
	}
	input, err := consoleInput(args, os.Stdin)
	if err != nil {
		return err
	}
	return commandrun.Command(ctx, installed, consoleservice.ContractName, args, input, environment, stdout, stderr)
}

// consoleInput reads the request a command takes on standard input. Only call
// and generate read one, and only when --input names no file.
func consoleInput(args []string, stdin io.Reader) ([]byte, error) {
	if len(args) == 0 || args[0] != "call" && args[0] != "generate" {
		return nil, nil
	}
	for index, argument := range args {
		if value, ok := strings.CutPrefix(argument, "--input="); ok && value != "-" {
			return nil, nil
		}
		if argument == "--input" && index+1 < len(args) && args[index+1] != "-" {
			return nil, nil
		}
	}
	data, err := io.ReadAll(io.LimitReader(stdin, commandkit.MaxStdinBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > commandkit.MaxStdinBytes {
		return nil, errors.New("console request exceeds 1 MiB")
	}
	return data, nil
}

// consoleServicePackage returns the installed package that serves
// console.service.
func consoleServicePackage() (store.Installed, error) {
	installed, err := installedPlugins()
	if err != nil {
		return store.Installed{}, err
	}
	for _, item := range installed {
		if item.Supports(consoleservice.ContractName, consoleservice.ContractVersion) {
			return item, nil
		}
	}
	return store.Installed{}, errors.New("no console service is installed; build ./pkg/console/cmd/console and install it with cymy plugins")
}

// consoleLaunchEnvironment builds the variables handed to the console service:
// the fixed passthrough names, plus each variable the command line or the
// configuration refers to by name (the operator or MCP token, a model API key).
func consoleLaunchEnvironment(args []string) []string {
	names := append([]string(nil), consolePassthroughEnvironment...)
	for index, argument := range args {
		flag, value, joined := strings.Cut(argument, "=")
		if !strings.HasPrefix(flag, "--") || !strings.HasSuffix(flag, "-env") {
			continue
		}
		if !joined && index+1 < len(args) {
			value = args[index+1]
		}
		if value = strings.TrimSpace(value); value != "" {
			names = append(names, value)
		}
	}
	names = append(names, consoleConfigEnvironmentNames(args)...)
	seen := map[string]bool{}
	var environment []string
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

// consoleConfigEnvironmentNames reads the names of the environment variables that
// the console configuration refers to (operator and MCP tokens, model API keys).
// The values come from this process's environment, never from the file.
func consoleConfigEnvironmentNames(args []string) []string {
	path := ""
	for index, argument := range args {
		if value, ok := strings.CutPrefix(argument, "--config="); ok {
			path = value
		} else if argument == "--config" && index+1 < len(args) {
			path = args[index+1]
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var document any
	if json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&document) != nil {
		return nil
	}
	var names []string
	var walk func(value any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, item := range typed {
				if text, ok := item.(string); ok && (key == "tokenEnvironment" || key == "apiKeyEnvironment") && text != "" {
					names = append(names, text)
					continue
				}
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(document)
	return names
}
