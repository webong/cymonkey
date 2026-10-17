package devicecli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"kit/devicekit"
)

func providerCommand(command string, args []string, output io.Writer, providers []devicekit.Provider) error {
	registry, err := devicekit.NewRegistry(providers...)
	if err != nil {
		return err
	}
	if command == "adapter-devices" {
		if len(args) != 0 {
			return errors.New("adapter-devices accepts no arguments")
		}
		devices, err := registry.List(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(map[string]any{"devices": devices})
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	provider := flags.String("adapter", "", "installed adapter name")
	device := flags.String("device", "", "adapter device ID")
	capability := flags.String("capability", "", "approved capability")
	resource := flags.String("resource", "", "approved resource handle")
	input := flags.String("input", "{}", "capability input JSON")
	maxBytes := flags.Int64("max-bytes", 16<<20, "maximum streamed bytes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*provider) == "" || strings.TrimSpace(*device) == "" || strings.TrimSpace(*capability) == "" {
		return errors.New("adapter action requires --adapter, --device, and --capability")
	}
	if !json.Valid([]byte(*input)) {
		return errors.New("--input must be JSON")
	}
	grant := devicekit.Grant{Capabilities: []devicekit.Capability{devicekit.Capability(*capability)}}
	if *resource != "" {
		grant.ResourceIDs = []string{*resource}
	}
	conn, err := registry.Open(context.Background(), devicekit.OpenRequest{ProviderID: *provider, DeviceID: *device, Grant: grant})
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	action := devicekit.Action{Capability: devicekit.Capability(*capability), ResourceID: *resource, Input: json.RawMessage(*input)}
	if command == "adapter-invoke" {
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
		return fmt.Errorf("adapter stream exceeded %d-byte host limit", *maxBytes)
	}
	return nil
}
