package contentcmd

import (
	"os"
	"path/filepath"
	"strings"

	"console/contentengine"
	contentplugin "console/contentengine/plugin"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
)

// pluginRoot is the store that holds the installed provider packages.
func pluginRoot() (string, error) {
	if value := os.Getenv("CYMONKEY_PLUGIN_DIR"); value != "" {
		return filepath.Abs(value)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cymonkey", "plugins"), nil
}

// contentPluginRegistry registers every installed content.provider package.
// Content is verified when a provider is opened, not when it is registered.
func contentPluginRegistry() (*contentengine.ProviderAdapterRegistry, error) {
	root, err := pluginRoot()
	if err != nil {
		return nil, err
	}
	packages, err := store.New(root, store.Limits{})
	if err != nil {
		return nil, err
	}
	installed, _, err := packages.ListUnverified()
	if err != nil {
		return nil, err
	}
	registry := contentengine.NewProviderAdapterRegistry()
	runtimeRoot, err := defaultContentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	for _, item := range installed {
		if !item.Supports(contentplugin.ContractName, contentplugin.ContractVersion) {
			continue
		}
		if err := contentplugin.RegisterWithRuntimeRoot(registry, item, runtimeRoot); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
