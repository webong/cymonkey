package browserextension

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPackageSafariProjectWithXcode(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("CYMONKEY_TEST_SAFARI_PACKAGER") == "" {
		t.Skip("set CYMONKEY_TEST_SAFARI_PACKAGER=1 to run the installed Xcode converter")
	}
	source := fixture(t)
	prepared, err := Inspect(source)
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "safari-project")
	result, err := PackageSafariProject(context.Background(), source, project, prepared.Revision, "com.example.cymonkey.testextension", "Test Extension")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "packaged" {
		t.Fatalf("unexpected Safari package result: %+v", result)
	}
	entries, err := os.ReadDir(project)
	if err != nil || len(entries) == 0 {
		t.Fatalf("Safari converter did not create a project at %s: %v", project, err)
	}
}

func TestInspectSignedSafariApp(t *testing.T) {
	app := os.Getenv("CYMONKEY_TEST_SAFARI_APP")
	if runtime.GOOS != "darwin" || app == "" {
		t.Skip("set CYMONKEY_TEST_SAFARI_APP to inspect an existing signed Safari extension app")
	}
	description, err := InspectSafariApp(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if description.ID == "" || description.Revision == "" {
		t.Fatalf("missing Safari app identity: %+v", description)
	}
}
