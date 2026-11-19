package boundary_test

import (
	"bufio"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The host (src) never imports a guest: guests reach it through the ext plugin
// system. Guests are the Go modules under pkg and the guest trees under src,
// which share the host's module but still run as separate plugin processes. Imports that remain from before the cut are recorded
// in guest_imports.txt, one "file import" pair per line. A new import fails the
// test; a recorded import that disappears fails too, so the list only shrinks.
// Regenerate it after removing imports with GUEST_IMPORTS_UPDATE=1.
const guestImportsFile = "guest_imports.txt"

// hostKits are modules under pkg that hold contracts and the host role for one
// domain (registries, launch, admission) with no engine or product behind
// them. A host may embed a kit, and so may a guest that hosts other plugins.
var hostKits = map[string]bool{"jangolova": true}

// guestTrees are guests that live in the host's module. Each builds its own
// plugin programs (cmd/...) and is reached only through the plugin system.
var guestTrees = []string{"src/board", "src/console"}

// guestPackage is the import path of a guest tree in the host's module.
func guestPackage(tree string) string { return "cymonkey/" + tree }

func TestHostImportsOnlyListedGuests(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve boundary test location")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	modules := guestModules(t, root)
	for _, tree := range guestTrees {
		modules = append(modules, guestPackage(tree))
	}

	found := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "src"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative, err := filepath.Rel(root, path); err == nil && isGuestTree(filepath.ToSlash(relative)) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			for _, module := range modules {
				if name == module || strings.HasPrefix(name, module+"/") {
					found[filepath.ToSlash(relative)+" "+name] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	listPath := filepath.Join(filepath.Dir(current), guestImportsFile)
	if os.Getenv("GUEST_IMPORTS_UPDATE") != "" {
		lines := make([]string, 0, len(found))
		for line := range found {
			lines = append(lines, line)
		}
		sort.Strings(lines)
		if err := os.WriteFile(listPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	allowed := map[string]bool{}
	file, err := os.Open(listPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			allowed[line] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for line := range found {
		if !allowed[line] {
			t.Errorf("host imports a guest module (reach it through the plugin system): %s", line)
		}
	}
	for line := range allowed {
		if !found[line] {
			t.Errorf("%s no longer imports it; remove %q from %s (GUEST_IMPORTS_UPDATE=1)", strings.Fields(line)[0], line, guestImportsFile)
		}
	}
}

// guestModules lists the Go module names declared under pkg.
func guestModules(t *testing.T, root string) []string {
	t.Helper()
	var modules []string
	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if entry.IsDir() || entry.Name() != "go.mod" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
				if name := strings.Trim(strings.TrimSpace(rest), `"`); !hostKits[name] {
					modules = append(modules, name)
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) == 0 {
		t.Fatal("no guest modules found under pkg")
	}
	return modules
}

func isGuestTree(relative string) bool {
	for _, tree := range guestTrees {
		if relative == tree {
			return true
		}
	}
	return false
}

// A guest tree in the host's module imports only its own packages from that
// module: never the host, and never another guest. Shared code lives in
// res/jangolova or ext.
func TestGuestTreesImportOnlyThemselves(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve boundary test location")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	for _, tree := range guestTrees {
		own := guestPackage(tree)
		checked := 0
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(tree)), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			checked++
			for _, imported := range parsed.Imports {
				name, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				if (name == "cymonkey" || strings.HasPrefix(name, "cymonkey/")) && name != own && !strings.HasPrefix(name, own+"/") {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("guest %s imports %s outside its own tree: %s", tree, name, filepath.ToSlash(rel))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if checked == 0 {
			t.Errorf("guest tree %s has no Go files", tree)
		}
	}
}
