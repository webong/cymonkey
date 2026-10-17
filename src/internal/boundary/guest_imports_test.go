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

// The host (src) never imports a guest: Go modules under pkg reach it through
// the ext plugin system. Imports that remain from before the cut are recorded
// in guest_imports.txt, one "file import" pair per line. A new import fails the
// test; a recorded import that disappears fails too, so the list only shrinks.
// Regenerate it after removing imports with GUEST_IMPORTS_UPDATE=1.
const guestImportsFile = "guest_imports.txt"

// hostKits are modules under pkg that hold contracts and the host role for one
// domain (registries, launch, admission) with no engine or product behind
// them. A host may embed a kit, and so may a guest that hosts other plugins.
var hostKits = map[string]bool{"kit": true}

func TestHostImportsOnlyListedGuests(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve boundary test location")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	modules := guestModules(t, root)

	found := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "src"), func(path string, entry fs.DirEntry, err error) error {
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
