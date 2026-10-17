package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host"
	"github.com/webong/ext/pkg/plugin-go/contract/host/process"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	"gopkg.in/yaml.v3"
	"jangolova/contentkit/service"
)

// contentCommandTimeout bounds one content command. Model and runtime installs
// download large files, so the bound is generous.
const contentCommandTimeout = 2 * time.Hour

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
// package hosts the content providers.
func runContent(args []string, stdout, stderr io.Writer) error {
	installed, err := contentServicePackage()
	if err != nil {
		return err
	}
	environment := contentLaunchEnvironment(args)
	executable, err := installed.Executable()
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "serve" {
		return runContentServe(installed, executable, args, environment, stdout, stderr)
	}
	proc, err := host.Open(context.Background(), installed, host.Options{
		Launch:    &process.Command{Path: executable, Env: environment},
		Authorize: contentServicePolicy,
		Timeout:   contentCommandTimeout,
	})
	if err != nil {
		return err
	}
	defer proc.Close()
	var result service.CommandResult
	if err := proc.Call(context.Background(), service.ContractName+".command", service.CommandRequest{Args: args}, &result); err != nil {
		return err
	}
	if _, err := stdout.Write(result.Stdout); err != nil {
		return err
	}
	if _, err := stderr.Write(result.Stderr); err != nil {
		return err
	}
	if result.Truncated {
		fmt.Fprintln(stderr, "content command output was truncated")
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return nil
}

// contentServicePolicy admits the one operation the content service serves.
func contentServicePolicy(_ context.Context, request plugin.Request) error {
	if request.Operation != "command" {
		return fmt.Errorf("content service operation %q is not allowed", request.Operation)
	}
	return nil
}

// runContentServe runs the long-lived HTTP service. It cannot return its
// output at the end of a call, so the verified executable runs as a child with
// this process's input and output and the same scrubbed environment.
func runContentServe(installed store.Installed, executable string, args, environment []string, stdout, stderr io.Writer) error {
	if err := installed.Verify(); err != nil {
		return err
	}
	command := exec.Command(executable, args...)
	command.Dir = installed.Directory
	command.Env = append([]string{"PATH=" + os.Getenv("PATH")}, environment...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, stdout, stderr
	return command.Run()
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
