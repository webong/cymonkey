package main

import (
	"encoding/json"
	"errors"
	"io"

	"jangolova/browserextension"
)

func browserCommand(args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != "targets" {
		return errors.New("browser requires targets")
	}
	targets, err := browserextension.DiscoverTargets()
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(targets)
}
