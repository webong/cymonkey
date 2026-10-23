package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	commandrun "jangolova/commandkit/run"
	gatewayservice "jangolova/gatewaykit/service"
)

// gatewayDefaultTokenEnvironment is the variable the gateway reads its token
// from when no --token-env is given.
const gatewayDefaultTokenEnvironment = "CYMONKEY_DEVICE_GATEWAY_TOKEN"

// gatewayPassthroughEnvironment names the host variables the gateway reads for
// its defaults. ext starts a plugin with a scrubbed environment.
var gatewayPassthroughEnvironment = []string{
	gatewayDefaultTokenEnvironment, "CYMONKEY_DEVICE_GATEWAY_BIND", "CYMONKEY_DISPLAY_UPSTREAM",
}

// gatewayCommand runs `cymy device gateway` in the installed gateway service
// package. The gateway serves until it is interrupted; its log lines stream back
// while it runs.
func gatewayCommand(args []string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return gatewayCommandContext(ctx, args, stdout, stderr)
}

// gatewayCommandContext is gatewayCommand with the context that stops the gateway.
func gatewayCommandContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	installed, err := gatewayServicePackage()
	if err != nil {
		return err
	}
	return commandrun.Command(ctx, installed, gatewayservice.ContractName, args, nil, gatewayLaunchEnvironment(args), stdout, stderr)
}

func gatewayServicePackage() (store.Installed, error) {
	installed, err := installedPlugins()
	if err != nil {
		return store.Installed{}, err
	}
	for _, item := range installed {
		if item.Supports(gatewayservice.ContractName, gatewayservice.ContractVersion) {
			return item, nil
		}
	}
	return store.Installed{}, errors.New("no display gateway is installed; build ./pkg/board/cmd/gateway and install it with cymy plugins")
}

// gatewayLaunchEnvironment passes the gateway only the token variable it will
// read and the defaults it consults.
func gatewayLaunchEnvironment(args []string) []string {
	names := append([]string(nil), gatewayPassthroughEnvironment...)
	for index, argument := range args {
		if value, ok := strings.CutPrefix(argument, "--token-env="); ok {
			names = append(names, strings.TrimSpace(value))
		} else if argument == "--token-env" && index+1 < len(args) {
			names = append(names, strings.TrimSpace(args[index+1]))
		}
	}
	seen := map[string]bool{}
	var environment []string
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}
