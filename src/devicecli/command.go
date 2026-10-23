// Package devicecli implements Cymonkey's public device commands.
package devicecli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"jangolova/devicekit"
)

type CommandEngine struct {
	Invocation string
	Stdin      io.Reader
	Providers  []devicekit.Provider
	// Keyboard returns the macOS keyboard provider bound to one process ID. The
	// host supplies it, so the CLI never links a device implementation.
	Keyboard func(pid int) (devicekit.Provider, error)
	// Gateway runs the display byte gateway command. The host supplies it, so
	// the CLI never links the gateway.
	Gateway func(args []string, stdout, stderr io.Writer) error
}

// Run executes the device command handlers. Provider actions still pass
// through Board's registry grant gate.
func (engine CommandEngine) Run(args []string, stdout, stderr io.Writer) error {
	invocation := strings.TrimSpace(engine.Invocation)
	if invocation == "" {
		return errors.New("device command engine requires an invocation name")
	}
	if len(args) == 0 {
		return fmt.Errorf("%s requires a command; use %s help", invocation, invocation)
	}
	switch args[0] {
	case "devices":
		return devicesCommand(args[1:], stdout)
	case "drive-list":
		return driveListCommand(args[1:], stdout)
	case "drive-read":
		return driveReadCommand(args[1:], stdout)
	case "keyboard-press":
		return keyboardPressCommand(args[1:], stdout, engine.Keyboard)
	case "keyboard-type":
		return keyboardTypeCommand(args[1:], engine.Stdin, stdout, engine.Keyboard)
	case "adapter-devices", "adapter-invoke", "adapter-stream":
		return providerCommand(args[0], args[1:], stdout, engine.Providers)
	case "gateway":
		if engine.Gateway == nil {
			return errors.New("this host has no display gateway")
		}
		return engine.Gateway(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		return usage(stderr, invocation)
	default:
		return fmt.Errorf("unknown %s command %q", invocation, args[0])
	}
}

func usage(w io.Writer, invocation string) error {
	_, err := fmt.Fprintf(w, `Usage: %s <command>

Commands:
  devices                 List host-approved mounted drives
  drive-list              List entries inside one host-approved drive root
  drive-read              Stream one file from a host-approved drive root to stdout
  keyboard-press          Submit one key to an explicit macOS process
  keyboard-type           Submit bounded UTF-8 text from stdin to an explicit macOS process
  adapter-devices         List installed adapter devices
  adapter-invoke          Invoke an installed adapter under an explicit grant
  adapter-stream          Stream bounded content from an installed adapter
  gateway                 Serve an authenticated display byte gateway

  Drive commands use --root NAME=PATH; keyboard commands use --pid PID.
  Cymonkey never discovers a drive or chooses a keyboard target.`, invocation)
	return err
}
