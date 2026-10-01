package displaybridge

import (
	"strings"
	"testing"

	"cymonkey/src/contracts"
	jangolovaregistry "jangolova/registry"
)

func TestRegistryConversionUsesCymonkeyContract(t *testing.T) {
	value := fromLibraryRegistry(jangolovaregistry.Registry{
		SchemaVersion: contracts.DisplayModuleRegistryVersion,
		RegistryID:    "fixture",
		Modules: []jangolovaregistry.Module{{
			ID: "render/example", Version: "1.0.0", Runtime: "example",
			ProtocolVersion: "cymonkey/v1alpha1", Status: "available",
		}},
	})
	if value.SchemaVersion != contracts.DisplayModuleRegistryVersion || value.Modules[0].ID != "render/example" {
		t.Fatalf("public registry = %#v", value)
	}
}

func TestModuleErrorsHideInternalLibraryName(t *testing.T) {
	err := publicError(jangolovaregistry.Registry{}.Validate())
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "jangolova") {
		t.Fatalf("public error = %v", err)
	}
}
