package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Worker selection belongs to the Cymonkey host.
func resolveBrowserWorker(kind string) (string, error) {
	if kind != "browser" {
		return "", errors.New("unsupported worker kind")
	}
	candidates := []string{
		strings.TrimSpace(os.Getenv("CYMONKEY_WORKER")),
		"scripts/cymonkey-worker.mjs",
		"/usr/local/lib/cymonkey/display/cymonkey-worker.mjs",
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", "lib", "cymonkey", "display", "cymonkey-worker.mjs"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(absolute); err == nil && !info.IsDir() {
			return absolute, nil
		}
	}
	return "", errors.New("Cymonkey browser worker not found; set CYMONKEY_WORKER")
}
