package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Worker layout and legacy environment settings belong to Cymonkey's host,
// not to a standalone Jangolova library.
func resolveBrowserWorker(kind string) (string, error) {
	if kind != "browser" {
		return "", errors.New("unsupported worker kind")
	}
	candidates := []string{
		strings.TrimSpace(os.Getenv("JANGOLOVA_CYMONKEY_WORKER")),
		"scripts/cymonkey-worker.mjs",
		"/usr/local/lib/jangolova/cymonkey-worker.mjs",
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", "lib", "jangolova", "cymonkey-worker.mjs"))
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
	return "", errors.New("Cymonkey browser worker not found; set JANGOLOVA_CYMONKEY_WORKER")
}
