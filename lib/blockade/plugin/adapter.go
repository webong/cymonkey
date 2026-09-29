// Package blockadeplugin adapts an installed executable to Blockade's
// provider interface. Blockade still validates every normalized response.
package blockadeplugin

import (
	"context"
	"time"

	"blockade"
	"providerplugin"
)

const Kind = "blockade.provider"

// RegisterInstalled selects Blockade adapters from a shared installation inventory.
func RegisterInstalled(registry *blockade.ProviderAdapterRegistry, installed []providerplugin.Installed) error {
	for _, item := range installed {
		if item.Manifest.Kind == Kind {
			if err := Register(registry, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func Register(registry *blockade.ProviderAdapterRegistry, installed providerplugin.Installed) error {
	return registry.Register(installed.Manifest.Name, func(ctx context.Context, runtime blockade.ProviderAdapterRuntime) (blockade.ProviderAdapter, error) {
		proc, err := providerplugin.Open(ctx, installed)
		if err != nil {
			return nil, err
		}
		if err := proc.Call(ctx, "blockade.start", map[string]any{"id": runtime.ID, "settings": runtime.Settings}, nil); err != nil {
			_ = proc.Close()
			return nil, err
		}
		return &adapter{proc: proc, runtime: runtime}, nil
	})
}

type adapter struct {
	proc    *providerplugin.Process
	runtime blockade.ProviderAdapterRuntime
}

func (a *adapter) Observe(ctx context.Context, request blockade.ProviderAdapterObserveRequest) (blockade.ProviderAdapterObserveResponse, error) {
	secrets := make(map[string]string, len(a.runtime.SecretNames))
	for _, name := range a.runtime.SecretNames {
		value, err := a.runtime.Secrets.Resolve(ctx, name)
		if err != nil {
			return blockade.ProviderAdapterObserveResponse{}, err
		}
		secrets[name] = value
	}
	var result blockade.ProviderAdapterObserveResponse
	err := a.proc.Call(ctx, "blockade.observe", map[string]any{"request": request, "secrets": secrets}, &result)
	return result, err
}
func (a *adapter) Capabilities(ctx context.Context) (blockade.ProviderAdapterCapabilities, error) {
	var value blockade.ProviderAdapterCapabilities
	err := a.proc.Call(ctx, "blockade.capabilities", nil, &value)
	return value, err
}
func (a *adapter) Health(ctx context.Context) (blockade.ProviderAdapterHealth, error) {
	var value blockade.ProviderAdapterHealth
	err := a.proc.Call(ctx, "blockade.health", nil, &value)
	return value, err
}
func (a *adapter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = a.proc.Call(ctx, "blockade.close", nil, nil)
	return a.proc.Close()
}
