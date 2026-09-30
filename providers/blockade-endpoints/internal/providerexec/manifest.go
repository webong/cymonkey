package providerexec

import (
	"path/filepath"

	"providerplugin"
)

func LoadManifest(executable string) (providerplugin.Manifest, error) {
	return providerplugin.LoadManifest(filepath.Join(filepath.Dir(executable), "plugin.json"))
}
