package providerexec

import (
	"path/filepath"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
)

// LoadDescriptor reads the package descriptor beside the provider executable.
func LoadDescriptor(executable string) (plugin.Descriptor, error) {
	return packagekit.LoadDescriptor(filepath.Join(filepath.Dir(executable), store.ManifestName))
}
