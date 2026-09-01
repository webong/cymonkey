package main

import (
	"os"

	"jangolova/internal/blockadecli"
)

func blockadeCommand(args []string) error {
	return blockadecli.Run(args, os.Stdout, os.Stderr)
}
