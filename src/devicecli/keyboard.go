package devicecli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"devicekit"
	devicehost "devicekit/host"
	"board/macoskeyboard"
)

const keyboardProviderID = "macos-keyboard"

func keyboardPressCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("keyboard-press", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pid := flags.Int("pid", 0, "explicit target process ID")
	key := flags.String("key", "", "named key or modified ANSI letter")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *pid <= 0 || strings.TrimSpace(*key) == "" {
		return errors.New("keyboard-press requires --pid PID and --key KEY, with no other arguments")
	}
	return invokeKeyboard(*pid, devicekit.KeyboardPress, map[string]string{"key": *key}, stdout)
}

func keyboardTypeCommand(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("keyboard-type", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pid := flags.Int("pid", 0, "explicit target process ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *pid <= 0 {
		return errors.New("keyboard-type requires --pid PID, with no other arguments")
	}
	if stdin == nil {
		return errors.New("keyboard-type requires UTF-8 text on stdin")
	}
	// A Unicode scalar needs at most four UTF-8 bytes. Read one byte beyond the
	// maximum so an oversized pipe cannot make this command consume unbounded
	// memory before Board validates the text.
	const maxInputBytes = 4 * macoskeyboard.DefaultMaxTextRunes
	data, err := io.ReadAll(io.LimitReader(stdin, maxInputBytes+1))
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > maxInputBytes {
		return fmt.Errorf("keyboard-type needs 1 to %d bytes of UTF-8 text", maxInputBytes)
	}
	return invokeKeyboard(*pid, devicekit.KeyboardType, map[string]string{"text": string(data)}, stdout)
}

func invokeKeyboard(pid int, capability devicekit.Capability, input any, stdout io.Writer) error {
	ctx := context.Background()
	keyboard, err := macoskeyboard.New(macoskeyboard.Options{ProviderID: keyboardProviderID, PID: pid})
	if err != nil {
		return err
	}
	devices, err := devicehost.NewRegistered(devicehost.Registration{Provider: keyboard})
	if err != nil {
		return err
	}
	defer devices.Close()
	connection, err := devices.Attach(ctx, devicehost.Request{
		Capabilities: []devicekit.Capability{capability},
	}, func(_ context.Context, _ devicekit.Device, _ devicehost.Request) (devicehost.Decision, error) {
		return devicehost.Decision{Capabilities: []devicekit.Capability{capability}}, nil
	})
	if err != nil {
		return err
	}
	defer connection.Close(ctx)
	encoded, err := json.Marshal(input)
	if err != nil {
		return err
	}
	result, err := connection.Invoke(ctx, devicekit.Action{Capability: capability, Input: encoded})
	if err != nil {
		return err
	}
	_, err = stdout.Write(append(result.Output, '\n'))
	return err
}
