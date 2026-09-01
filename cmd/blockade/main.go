package main

import (
	"fmt"
	"os"

	"jangolova/internal/blockadecli"
)

func main() {
	if err := blockadecli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "blockade: %v\n", err)
		os.Exit(1)
	}
}
