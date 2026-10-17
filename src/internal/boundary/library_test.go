package boundary_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Library-specific integration is composed outside src/internal. Private
// packages must not acquire a direct dependency on a standalone library.
func TestLibrariesStayOutsideInternal(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve boundary test location")
	}
	internal := filepath.Clean(filepath.Join(filepath.Dir(current), ".."))
	err := filepath.WalkDir(internal, func(path string, entry fs.DirEntry, err error) error {
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
		for _, imported := range parsed.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			for _, library := range []string{"board", "blockade", "jangolova"} {
				if name == library || strings.HasPrefix(name, library+"/") {
					t.Errorf("internal package imports %s: %s", library, path)
				}
			}
		}
		if !strings.HasSuffix(path, "_test.go") {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			lower := bytes.ToLower(contents)
			for _, library := range []string{"board", "blockade", "jangolova"} {
				if bytes.Contains(lower, []byte(library)) {
					t.Errorf("internal package names standalone library %s: %s", library, path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHostConfigurationUsesServiceContracts(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve boundary test location")
	}
	host := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "host"))
	err := filepath.WalkDir(host, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lower := bytes.ToLower(data)
		for _, library := range []string{"blockade", "jangolova", "board"} {
			if bytes.Contains(lower, []byte(library)) {
				t.Errorf("internal host configuration names library %s: %s", library, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInternalLibrariesDoNotShipCommands(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve boundary test location")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	for _, name := range []string{"jangolova", "blockade", "board"} {
		library := filepath.Join(root, "pkg", name)
		for _, commandDirectory := range []string{"cli", "cmd"} {
			if _, err := os.Stat(filepath.Join(library, commandDirectory)); err == nil {
				t.Errorf("internal library %s owns a %s command directory", name, commandDirectory)
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		err := filepath.WalkDir(library, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly)
			if err != nil {
				return err
			}
			if parsed.Name.Name == "main" {
				t.Errorf("internal library %s ships command package: %s", name, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
