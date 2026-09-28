package providerplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func DefaultRoot() (string, error) {
	if value := os.Getenv("CYMONKEY_PLUGIN_DIR"); value != "" {
		return filepath.Abs(value)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cymonkey", "plugins"), nil
}

// Install copies a reviewed local executable and its manifest into the user
// plugin directory. Discovery never executes a plugin and installation does
// not replace an existing name.
func Install(root, manifestPath string) (Installed, error) {
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return Installed{}, err
	}
	source := Installed{Manifest: m, Directory: filepath.Dir(manifestPath)}
	if _, err := source.executable(); err != nil {
		return Installed{}, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return Installed{}, err
	}
	destination := filepath.Join(root, m.Name)
	if _, err := os.Lstat(destination); err == nil {
		return Installed{}, fmt.Errorf("plugin %q is already installed", m.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Installed{}, err
	}
	staging, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return Installed{}, err
	}
	defer os.RemoveAll(staging)
	input, err := os.Open(filepath.Join(source.Directory, m.Command))
	if err != nil {
		return Installed{}, err
	}
	defer input.Close()
	output, err := os.OpenFile(filepath.Join(staging, m.Command), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return Installed{}, err
	}
	h := sha256.New()
	n, err := io.Copy(output, io.TeeReader(io.LimitReader(input, (128<<20)+1), h))
	closeErr := output.Close()
	if err != nil {
		return Installed{}, err
	}
	if closeErr != nil {
		return Installed{}, closeErr
	}
	if n > 128<<20 || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return Installed{}, errors.New("plugin executable changed during install")
	}
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Installed{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "plugin.json"), append(encoded, '\n'), 0600); err != nil {
		return Installed{}, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return Installed{}, err
	}
	return Installed{Manifest: m, Directory: destination}, nil
}

// Upgrade installs a new local revision of the same plugin. The prior
// installation remains available if staging or replacement fails.
func Upgrade(root, manifestPath string) (Installed, error) {
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return Installed{}, err
	}
	destination := filepath.Join(root, m.Name)
	old, err := LoadManifest(filepath.Join(destination, "plugin.json"))
	if err != nil {
		return Installed{}, err
	}
	if old.Name != m.Name || old.Kind != m.Kind {
		return Installed{}, errors.New("plugin upgrade must keep name and kind")
	}
	if old.Version == m.Version {
		return Installed{}, errors.New("plugin upgrade requires a new version")
	}
	stagingRoot, err := os.MkdirTemp(root, ".upgrade-")
	if err != nil {
		return Installed{}, err
	}
	defer os.RemoveAll(stagingRoot)
	staged, err := Install(stagingRoot, manifestPath)
	if err != nil {
		return Installed{}, err
	}
	backupRoot, err := os.MkdirTemp(root, ".backup-")
	if err != nil {
		return Installed{}, err
	}
	backup := filepath.Join(backupRoot, m.Name)
	if err := os.Rename(destination, backup); err != nil {
		_ = os.Remove(backupRoot)
		return Installed{}, err
	}
	if err := os.Rename(staged.Directory, destination); err != nil {
		if restoreErr := os.Rename(backup, destination); restoreErr != nil {
			return Installed{}, errors.Join(err, fmt.Errorf("previous plugin retained at %s: %w", backup, restoreErr))
		}
		_ = os.Remove(backupRoot)
		return Installed{}, err
	}
	_ = os.RemoveAll(backupRoot)
	return Installed{Manifest: m, Directory: destination}, nil
}

func Remove(root, name string) error {
	if !namePattern.MatchString(name) {
		return errors.New("plugin name is invalid")
	}
	p := Installed{Directory: filepath.Join(root, name)}
	m, err := LoadManifest(filepath.Join(p.Directory, "plugin.json"))
	if err != nil {
		return err
	}
	if m.Name != name {
		return errors.New("plugin name does not match directory")
	}
	// The host only removes its own per-plugin directory. Installation never
	// follows links, and a symlinked plugin directory is rejected here.
	info, err := os.Lstat(p.Directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("plugin directory is not a directory")
	}
	return os.RemoveAll(p.Directory)
}
