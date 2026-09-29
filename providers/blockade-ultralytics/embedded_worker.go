package main

import (
	_ "embed"
	"os"
	"path/filepath"
)

// The provider executable carries its Python bridge so plugin installation does
// not depend on a source checkout. The host still needs Python and the runtime
// packages listed in python/requirements.txt.

//go:embed python/worker.py
var embeddedWorker []byte

//go:embed python/runtime.py
var embeddedRuntime []byte

func embeddedWorkerCommand() ([]string, func(), error) {
	directory, err := os.MkdirTemp("", "blockade-ultralytics-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	if err := os.WriteFile(filepath.Join(directory, "worker.py"), embeddedWorker, 0600); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(directory, "runtime.py"), embeddedRuntime, 0600); err != nil {
		cleanup()
		return nil, nil, err
	}
	return []string{"python3", filepath.Join(directory, "worker.py")}, cleanup, nil
}
