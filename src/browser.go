package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
)

func browserCommand(args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != "targets" {
		return errors.New("browser requires targets")
	}
	targets, err := discoverBrowserTargets(context.Background())
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(targets)
}
