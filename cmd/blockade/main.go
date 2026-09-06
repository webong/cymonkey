package main

import (
	"fmt"
	"os"

	"cymonkey/internal/blockade"
	"cymonkey/internal/blockadecli"
	"cymonkey/internal/blockadewebllm"
)

func main() {
	registry := blockade.NewProviderAdapterRegistry()
	if err := blockadewebllm.Register(registry); err != nil {
		fmt.Fprintf(os.Stderr, "blockade: %v\n", err)
		os.Exit(1)
	}
	if err := blockadecli.RunWithProviderAdapters(os.Args[1:], os.Stdout, os.Stderr, registry, blockade.EnvironmentSecretResolver{}); err != nil {
		fmt.Fprintf(os.Stderr, "blockade: %v\n", err)
		os.Exit(1)
	}
}
