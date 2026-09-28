package boardcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"board"
)

func providerCommand(command string, args []string, output io.Writer, providers []board.Provider) error {
	registry, err := board.NewRegistry(providers...)
	if err != nil {
		return err
	}
	if command == "provider-devices" {
		if len(args) != 0 {
			return errors.New("provider-devices accepts no arguments")
		}
		devices, err := registry.List(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(map[string]any{"devices": devices})
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	provider := flags.String("provider", "", "installed provider name")
	device := flags.String("device", "", "provider device ID")
	capability := flags.String("capability", "", "approved capability")
	resource := flags.String("resource", "", "approved resource handle")
	input := flags.String("input", "{}", "capability input JSON")
	maxBytes := flags.Int64("max-bytes", 16<<20, "maximum streamed bytes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*provider) == "" || strings.TrimSpace(*device) == "" || strings.TrimSpace(*capability) == "" {
		return errors.New("provider action requires --provider, --device, and --capability")
	}
	if !json.Valid([]byte(*input)) {
		return errors.New("--input must be JSON")
	}
	grant := board.Grant{Capabilities: []board.Capability{board.Capability(*capability)}}
	if *resource != "" {
		grant.ResourceIDs = []string{*resource}
	}
	conn, err := registry.Open(context.Background(), board.OpenRequest{ProviderID: *provider, DeviceID: *device, Grant: grant})
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	action := board.Action{Capability: board.Capability(*capability), ResourceID: *resource, Input: json.RawMessage(*input)}
	if command == "provider-invoke" {
		result, err := conn.Invoke(context.Background(), action)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(result)
	}
	if *maxBytes <= 0 || *maxBytes > 128<<20 {
		return errors.New("--max-bytes must be between 1 and 128 MiB")
	}
	stream, err := conn.Stream(context.Background(), action)
	if err != nil {
		return err
	}
	defer stream.Close()
	n, err := io.Copy(output, io.LimitReader(stream, *maxBytes+1))
	if err != nil {
		return err
	}
	if n > *maxBytes {
		return fmt.Errorf("provider stream exceeded %d-byte host limit", *maxBytes)
	}
	return nil
}
