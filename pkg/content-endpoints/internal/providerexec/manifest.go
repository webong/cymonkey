package providerexec

import (
	"path/filepath"

	plugin "github.com/webong/ext/pkg/ext-go/engine"
	"github.com/webong/ext/pkg/ext-go/engine/host/store"
	packagekit "github.com/webong/ext/pkg/ext-go/engine/package"
)

// LoadDescriptor reads the package descriptor beside the provider executable.
func LoadDescriptor(executable string) (plugin.Descriptor, error) {
	return packagekit.LoadDescriptor(filepath.Join(filepath.Dir(executable), store.ManifestName))
}
