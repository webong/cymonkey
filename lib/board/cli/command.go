// Package boardcli provides Board's standalone command surface. A host can
// route its own executable to Run without placing device logic in that host.
package boardcli

import (
	"errors"
	"fmt"
	"io"

	"board"
)

func Run(args []string, stdout, stderr io.Writer) error {
	return RunWithInput(args, nil, stdout, stderr)
}

// RunWithInput supplies stdin for keyboard.type. Other commands do not read it.
func RunWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return RunWithProviders(args, stdin, stdout, stderr, nil)
}

// RunWithProviders adds caller-installed providers to Board's command surface.
// Provider actions still pass through Board's registry grant gate.
func RunWithProviders(args []string, stdin io.Reader, stdout, stderr io.Writer, providers []board.Provider) error {
	if len(args) == 0 {
		return errors.New("board requires a command; use board help")
	}
	switch args[0] {
	case "devices":
		return devicesCommand(args[1:], stdout)
	case "drive-list":
		return driveListCommand(args[1:], stdout)
	case "drive-read":
		return driveReadCommand(args[1:], stdout)
	case "keyboard-press":
		return keyboardPressCommand(args[1:], stdout)
	case "keyboard-type":
		return keyboardTypeCommand(args[1:], stdin, stdout)
	case "provider-devices", "provider-invoke", "provider-stream":
		return providerCommand(args[0], args[1:], stdout, providers)
	case "help", "-h", "--help":
		return usage(stderr)
	default:
		return fmt.Errorf("unknown board command %q", args[0])
	}
}

func usage(w io.Writer) error {
	_, err := fmt.Fprintln(w, `Usage: board <command>

Commands:
  devices                 List host-approved mounted drives
  drive-list              List entries inside one host-approved drive root
  drive-read              Stream one file from a host-approved drive root to stdout
  keyboard-press          Submit one key to an explicit macOS process
  keyboard-type           Submit bounded UTF-8 text from stdin to an explicit macOS process
  provider-devices        List installed provider devices
  provider-invoke         Invoke an installed provider under an explicit grant
  provider-stream         Stream bounded content from an installed provider

  Drive commands use --root NAME=PATH; keyboard commands use --pid PID.
  Board never discovers a drive or chooses a keyboard target.`)
	return err
}
