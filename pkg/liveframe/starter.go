package liveframe

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed starter/index.html
var starterPage []byte

// InstallStarter creates the Liveframe bridge page in a caller-selected
// workspace. It never overwrites an existing index.html or starts Canvas.
func InstallStarter(workspaceDir string) (string, error) {
	if !filepath.IsAbs(workspaceDir) {
		return "", errors.New("Liveframe workspace directory must be absolute")
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return "", fmt.Errorf("create Liveframe workspace: %w", err)
	}
	path := filepath.Join(workspaceDir, "index.html")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("create Liveframe starter without overwriting: %w", err)
	}
	if _, err := file.Write(starterPage); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write Liveframe starter: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close Liveframe starter: %w", err)
	}
	return path, nil
}
