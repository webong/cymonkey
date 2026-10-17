// provider serves Board's device implementations as an ext package that
// speaks the device.provider contract. The host chooses what it serves through
// non-secret settings sent with list and open:
//
//	inventory    absolute path of a device inventory file (see board/config)
//	keyboardPid  process ID that the macOS keyboard provider may type into
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"board/config"
	"board/macoskeyboard"
	"jangolova/devicekit"
	"jangolova/devicekit/serve"
)

const keyboardProviderID = "macos-keyboard"

func main() {
	if err := serve.Run(context.Background(), providers); err != nil {
		fmt.Fprintln(os.Stderr, "board provider:", err)
		os.Exit(1)
	}
}

func providers(_ context.Context, settings map[string]string) ([]devicekit.Provider, error) {
	var result []devicekit.Provider
	if path := settings["inventory"]; path != "" {
		if !filepath.IsAbs(path) {
			return nil, errors.New("inventory path must be absolute")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, errors.New("open device inventory")
		}
		defer file.Close()
		configured, err := config.Load(file)
		if err != nil {
			return nil, err
		}
		result = append(result, configured...)
	}
	if value := settings["keyboardPid"]; value != "" {
		pid, err := strconv.Atoi(value)
		if err != nil {
			return nil, errors.New("keyboardPid must be a process ID")
		}
		keyboard, err := macoskeyboard.New(macoskeyboard.Options{ProviderID: keyboardProviderID, PID: pid})
		if err != nil {
			return nil, err
		}
		result = append(result, keyboard)
	}
	return result, nil
}
