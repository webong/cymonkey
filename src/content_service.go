package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	"gopkg.in/yaml.v3"
	commandrun "jangolova/commandkit/run"
	"jangolova/contentkit/service"
)

// contentPassthroughEnvironment names the host variables the content engine
// needs to find its configuration and storage. ext starts a plugin with a
// scrubbed environment, so nothing else reaches it unless a command or the
// content configuration names it.
var contentPassthroughEnvironment = []string{
	"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME",
	"TMPDIR", "TEMP", "TMP",
	"CYMONKEY_PLUGIN_DIR", "CYMONKEY_RUNTIME_DIR", "CYMONKEY_MODEL_DIR",
	"CYMONKEY_CONTENT_CONFIG", "CYMONKEY_CONTENT_BIND",
	"BLOCKADE_MODEL_DIR", "BLOCKADE_RUNTIME_DIR",
}

// runContent runs a public `cymy content` command in the installed content
// service package. The host decides which operations the package may serve; the
// package hosts the content providers. The command's log lines stream back as it
// runs, and an interrupt cancels it.
func runContent(args []string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runContentContext(ctx, args, stdout, stderr)
}

// runContentContext is runContent with the context that cancels the command.
func runContentContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	installed, err := contentServicePackage()
	if err != nil {
		return err
	}
	return commandrun.Command(ctx, installed, service.ContractName, args, nil, contentLaunchEnvironment(args), stdout, stderr)
}

// contentServicePolicy admits the one operation the content service serves.
func contentServicePolicy(ctx context.Context, request plugin.Request) error {
	return commandrun.Policy(ctx, request)
}

// contentServicePackage returns the installed package that serves
// content.service.
func contentServicePackage() (store.Installed, error) {
	installed, err := installedPlugins()
	if err != nil {
		return store.Installed{}, err
	}
	for _, item := range installed {
		if item.Supports(service.ContractName, service.ContractVersion) {
			return item, nil
		}
	}
	return store.Installed{}, errors.New("no content service is installed; build ./pkg/console/cmd/content and install it with cymy plugins")
}

// contentLaunchEnvironment builds the variables handed to the content service:
// the fixed passthrough names, plus any variable the command line or the content
// configuration refers to by name, such as a provider API key or a token.
func contentLaunchEnvironment(args []string) []string {
	names := append([]string(nil), contentPassthroughEnvironment...)
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
	names = append(names, contentConfigSecretNames(args)...)
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

// contentConfigSecretNames reads the names of the environment variables that
// the content configuration lists under adapter secrets. The values are not
// read from the file; they come from this process's environment.
func contentConfigSecretNames(args []string) []string {
	path := strings.TrimSpace(os.Getenv("CYMONKEY_CONTENT_CONFIG"))
	for index, argument := range args {
		if value, ok := strings.CutPrefix(argument, "--config="); ok {
			path = value
		} else if argument == "--config" && index+1 < len(args) {
			path = args[index+1]
		}
	}
	if path == "" {
		directory, err := os.UserConfigDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(directory, "cymonkey", "content.yaml")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return nil
	}
	var config struct {
		Adapters []struct {
			Secrets map[string]struct {
				Env string `yaml:"env"`
			} `yaml:"secrets"`
		} `yaml:"adapters"`
	}
	if yaml.Unmarshal(data, &config) != nil {
		return nil
	}
	var names []string
	for _, adapter := range config.Adapters {
		for _, secret := range adapter.Secrets {
			if secret.Env != "" {
				names = append(names, secret.Env)
			}
		}
	}
	return names
}
