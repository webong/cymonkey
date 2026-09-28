package boundary_test

import (
	"go/parser"
	"go/token"
	"io/fs"
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
			for _, library := range []string{"board", "blockade", "jangolova", "providerplugin"} {
				if name == library || strings.HasPrefix(name, library+"/") {
					t.Errorf("internal package imports %s: %s", library, path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
