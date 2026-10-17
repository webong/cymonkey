package devicekit

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDeviceKitDoesNotDependOnCymonkey keeps Board usable without Cymonkey. Board
// must resolve from its own module with no private host package in the graph,
// otherwise a caller who already owns a device could not use the library.
func TestDeviceKitDoesNotDependOnCymonkey(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("dependency graph: %v: %s", err, out)
	}
	for _, dependency := range strings.Fields(string(out)) {
		for _, prefix := range []string{"cymonkey", "jangolova", "contentkit", "generationkit", "board", "github.com/xallet/xallet"} {
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Errorf("device kit depends on a host or sibling library: %s", dependency)
			}
		}
	}
}
