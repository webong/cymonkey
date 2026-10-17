package blockadeplugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extplugin "github.com/webong/ext/pkg/plugin-go/contract"
	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
)

func op(name string) extplugin.Request { return extplugin.Request{Operation: name} }

func TestWorkerStartsOnceBeforeAnythingElse(t *testing.T) {
	authorize := workerPolicy()
	ctx := context.Background()
	for _, operation := range []string{"observe", "interpret", "capabilities", "health", "close", "model.inspect"} {
		if err := authorize(ctx, op(operation)); err == nil {
			t.Fatalf("a worker admitted %s before start", operation)
		}
	}
	if err := authorize(ctx, op("start")); err != nil {
		t.Fatal(err)
	}
	if err := authorize(ctx, op("start")); err == nil {
		t.Fatal("a worker admitted start twice")
	}
	for _, operation := range []string{"capabilities", "health", "observe", "interpret", "close"} {
		if err := authorize(ctx, op(operation)); err != nil {
			t.Fatalf("a started worker refused %s: %v", operation, err)
		}
	}
	if err := authorize(ctx, op("model.inspect")); err == nil {
		t.Fatal("a worker admitted model.inspect")
	}
}

func TestInspectProcessAdmitsOnlyModelInspect(t *testing.T) {
	authorize := inspectPolicy()
	ctx := context.Background()
	if err := authorize(ctx, op("model.inspect")); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"start", "observe", "interpret", "close"} {
		if err := authorize(ctx, op(operation)); err == nil {
			t.Fatalf("an inspect process admitted %s", operation)
		}
	}
}

func sourceInstalled(t *testing.T) (store.Installed, string) {
	t.Helper()
	source := t.TempDir()
	descriptor := `{"apiVersion":"host.content-source/v1alpha1","runtime":{"name":"python","version":"3.12.8"},"entrypoint":"python/provider.py"}`
	for name, content := range map[string]string{"content.json": descriptor, "python/provider.py": "print('x')\n", "python/Vendor/Mixed.PY": "VALUE = 1\n"} {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := packagekit.SourceManifest("tree-fixture", "1.0.0", []extplugin.Contract{testContract("host.content", Operations)}, source, []string{"content.json", "python"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteManifest(source, manifest); err != nil {
		t.Fatal(err)
	}
	packages, err := store.New(t.TempDir(), store.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := packages.Install(source)
	if err != nil {
		t.Fatal(err)
	}
	return installed, filepath.Join(installed.Directory, "python", "provider.py")
}

func TestPackageFileResolvesInsideATreeWithoutHashingIt(t *testing.T) {
	installed, provider := sourceInstalled(t)
	got, err := packageFile(installed, "python/provider.py")
	if err != nil || got != provider {
		t.Fatalf("packageFile = %q %v", got, err)
	}
	if _, err := packageFile(installed, "python/Vendor/Mixed.PY"); err != nil {
		t.Fatalf("a mixed-case file inside the tree was refused: %v", err)
	}
	for _, bad := range []string{"../escape.py", "/etc/passwd", "python/../content.json", "python/missing.py", "python", "other/file.py"} {
		if _, err := packageFile(installed, bad); err == nil {
			t.Fatalf("packageFile accepted %q", bad)
		}
	}
	// Resolving a path does not verify content, so a tampered tree still
	// resolves here; the whole-package verification that host.Open runs before
	// it starts anything is what refuses it.
	if err := os.WriteFile(provider, []byte("print('tampered')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := packageFile(installed, "python/provider.py"); err != nil {
		t.Fatalf("resolving a tampered file failed: %v", err)
	}
	if err := installed.Verify(); err == nil {
		t.Fatal("a tampered tree still verified, so a launch would not have refused it")
	}
}
