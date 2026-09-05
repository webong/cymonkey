package main

import (
	"os"

	"cymonkey/internal/blockadecli"
)

func blockadeCommand(args []string) error {
	return blockadecli.Run(args, os.Stdout, os.Stderr)
}
