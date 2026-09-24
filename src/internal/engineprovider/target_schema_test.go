package engineprovider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTargetSchemaIsProviderAndLocationNeutral(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "protocol", "target", "v1")
	for _, name := range []string{"target.schema.json", "connection-material.schema.json"} {
		contents, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(contents) {
			t.Fatalf("%s is invalid JSON", name)
		}
		lower := strings.ToLower(string(contents))
		for _, forbidden := range []string{"jangolova", "xallet", "localhost", "container", "virtual-machine"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("provider-neutral schema %s contains %q", name, forbidden)
			}
		}
	}
}
