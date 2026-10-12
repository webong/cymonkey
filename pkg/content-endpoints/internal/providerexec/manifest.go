package providerexec

import (
	"path/filepath"

	"github.com/webong/ext/pkg/plugin"
	"github.com/webong/ext/pkg/plugin/packagekit"
	"github.com/webong/ext/pkg/plugin/store"
)

// LoadDescriptor reads the package descriptor beside the provider executable.
func LoadDescriptor(executable string) (plugin.Descriptor, error) {
	return packagekit.LoadDescriptor(filepath.Join(filepath.Dir(executable), store.ManifestName))
}
