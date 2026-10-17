//go:build !windows

package main

import (
	"fmt"
	"os"
)

// The helper drives the interactive Windows desktop, so it only does work on
// Windows. This stub keeps the package buildable and testable elsewhere.
func main() {
	fmt.Fprintln(os.Stderr, "Cymonkey Windows helper runs only on Windows")
	os.Exit(2)
}
