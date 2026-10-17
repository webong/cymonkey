package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"cymonkey/src/devicebridge"
	devicecli "cymonkey/src/devicecli"
	"jangolova/devicekit"
)

// runDevice is Cymonkey's adapter from the public device command to the
// internal device library.
func runDevice(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && strings.HasPrefix(args[0], "adapter-") {
		providers, err := devicePluginAdapters()
		if err != nil {
			return publicDeviceError(err)
		}
		engine := devicecli.CommandEngine{Invocation: "cymy device", Stdin: os.Stdin, Providers: providers, Keyboard: keyboardPluginProvider}
		if args[0] == "adapter-stream" {
			return publicDeviceError(engine.Run(args, stdout, stderr))
		}
		var libraryOutput bytes.Buffer
		if err := engine.Run(args, &libraryOutput, stderr); err != nil {
			return publicDeviceError(err)
		}
		if args[0] == "adapter-devices" {
			var response struct {
				Devices []devicekit.Device `json:"devices"`
			}
			if err := json.Unmarshal(libraryOutput.Bytes(), &response); err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(devicebridge.FromLibraryDevices(response.Devices))
		}
		var result devicekit.Result
		if err := json.Unmarshal(libraryOutput.Bytes(), &result); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(devicebridge.FromLibraryResult(result))
	}
	if len(args) > 0 && args[0] == "devices" && hasDeviceFlag(args, "json") {
		var libraryOutput bytes.Buffer
		if err := (devicecli.CommandEngine{Invocation: "cymy device", Stdin: os.Stdin, Keyboard: keyboardPluginProvider}).Run(args, &libraryOutput, stderr); err != nil {
			return publicDeviceError(err)
		}
		var response struct {
			Devices []devicekit.Device `json:"devices"`
		}
		if err := json.Unmarshal(libraryOutput.Bytes(), &response); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(devicebridge.FromLibraryDevices(response.Devices))
	}
	return publicDeviceError((devicecli.CommandEngine{Invocation: "cymy device", Stdin: os.Stdin, Keyboard: keyboardPluginProvider}).Run(args, stdout, stderr))
}

func hasDeviceFlag(args []string, name string) bool {
	for _, argument := range args {
		if argument == "--"+name || strings.HasPrefix(argument, "--"+name+"=") {
			return true
		}
	}
	return false
}

func publicDeviceError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.NewReplacer(
		"Board provider", "device adapter",
		"board provider", "device adapter",
		"Board", "device",
		"board", "device",
	).Replace(err.Error())
	return fmt.Errorf("%s", message)
}
