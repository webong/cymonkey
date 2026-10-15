// Package paths names the directories where Cymonkey keeps its own data.
package paths

import (
	"os"
	"path/filepath"
)

// UserscriptDirectory is where Cymonkey stores approved userscripts. The
// stored record format and store logic belong to ext's userscript package;
// only the location is Cymonkey's.
func UserscriptDirectory() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cymonkey", "userscripts"), nil
}
