// Package displaybridge converts Cymonkey's public display contracts to the
// standalone display library's native contracts.
package displaybridge

import (
	"context"
	"errors"
	"strings"

	jangolovaregistry "board/display/registry"
	"cymonkey/src/contracts"
)

func DiscoverModules(ctx context.Context, endpoint string) (contracts.DisplayModuleRegistry, error) {
	registry, err := jangolovaregistry.DiscoverWithSchemaVersion(
		ctx, endpoint, contracts.DisplayModuleRegistryVersion,
		jangolovaregistry.DiscoveryOptions{},
	)
	if err != nil {
		return contracts.DisplayModuleRegistry{}, publicError(err)
	}
	return fromLibraryRegistry(registry), nil
}

func PullModule(ctx context.Context, endpoint, moduleID, platform, cache string) (contracts.DisplayModulePullResult, error) {
	registry, err := jangolovaregistry.DiscoverWithSchemaVersion(
		ctx, endpoint, contracts.DisplayModuleRegistryVersion,
		jangolovaregistry.DiscoveryOptions{},
	)
	if err != nil {
		return contracts.DisplayModulePullResult{}, publicError(err)
	}
	module, err := registry.Select(moduleID, platform)
	if err != nil {
		return contracts.DisplayModulePullResult{}, publicError(err)
	}
	path, err := jangolovaregistry.Pull(ctx, module, platform, cache, jangolovaregistry.PullOptions{})
	if err != nil {
		return contracts.DisplayModulePullResult{}, publicError(err)
	}
	return contracts.DisplayModulePullResult{Module: module.ID, Version: module.Version, Path: path}, nil
}

func fromLibraryRegistry(registry jangolovaregistry.Registry) contracts.DisplayModuleRegistry {
	modules := make([]contracts.DisplayModule, len(registry.Modules))
	for index, module := range registry.Modules {
		artifacts := make([]contracts.DisplayModuleArtifact, len(module.Artifacts))
		for artifactIndex, artifact := range module.Artifacts {
			artifacts[artifactIndex] = contracts.DisplayModuleArtifact{
				Platform: artifact.Platform, URL: artifact.URL, SHA256: artifact.SHA256,
				MediaType: artifact.MediaType, Size: artifact.Size,
			}
		}
		modules[index] = contracts.DisplayModule{
			ID: module.ID, Version: module.Version, Runtime: module.Runtime,
			ProtocolVersion: module.ProtocolVersion, Status: module.Status,
			Platforms: append([]string(nil), module.Platforms...),
			Actions:   append([]string(nil), module.Actions...), Artifacts: artifacts,
		}
	}
	return contracts.DisplayModuleRegistry{
		SchemaVersion: contracts.DisplayModuleRegistryVersion,
		RegistryID:    registry.RegistryID,
		Modules:       modules,
	}
}

func publicError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(strings.NewReplacer("Jangolova", "display module", "jangolova", "display-module").Replace(err.Error()))
}
