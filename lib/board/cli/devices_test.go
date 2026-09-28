package boardcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func populatedDrive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"IMG_0001.JPG":               "jpeg",
		"DCIM/100CANON/IMG_0002.JPG": "jpeg2",
		"DCIM/100CANON/notes.txt":    "text",
	}
	for name, contents := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDeviceRootFlags(t *testing.T) {
	t.Parallel()

	var roots driveRootFlags
	if err := roots.Set("photos=/Volumes/NOAA"); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].ResourceID != "photos" || roots[0].Path != "/Volumes/NOAA" {
		t.Fatalf("roots = %#v", roots)
	}
	if !strings.Contains(roots.String(), "photos=/Volumes/NOAA") {
		t.Fatalf("String() = %q", roots.String())
	}
	for _, invalid := range []string{
		"", "photos", "=/Volumes/NOAA", "photos=", "photos=/a=/b",
		"1photos=/Volumes/NOAA", "photos with space=/Volumes/NOAA",
	} {
		if err := roots.Set(invalid); err == nil {
			t.Fatalf("root %q was accepted", invalid)
		}
	}
	// A handle already in use is ambiguous, so it is refused rather than shadowed.
	if err := roots.Set("photos=/Volumes/OTHER"); err == nil {
		t.Fatal("the same root handle was accepted twice")
	}
}

func TestDevicesCommandReportsHostApprovedMounts(t *testing.T) {
	dir := populatedDrive(t)
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"devices", "--root", "photos=" + dir}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, "mounted-drive-photos/drive") {
		t.Fatalf("devices output = %q", output)
	}
	if !strings.Contains(output, "drive.list,drive.read") {
		t.Fatalf("devices output did not advertise capabilities: %q", output)
	}
	// The approved path is the provider's business and is not published.
	if strings.Contains(output, dir) {
		t.Fatalf("devices output published the host path: %q", output)
	}

	stdout.Reset()
	if err := Run([]string{"devices", "--root", "photos=" + dir, "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Devices []struct {
			ProviderID   string   `json:"providerId"`
			ID           string   `json:"id"`
			Kind         string   `json:"kind"`
			ResourceIDs  []string `json:"resourceIds"`
			Capabilities []struct {
				Name        string          `json:"name"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"capabilities"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Devices) != 1 || len(inventory.Devices[0].ResourceIDs) != 1 {
		t.Fatalf("inventory = %#v", inventory)
	}
	if inventory.Devices[0].ResourceIDs[0] != "photos" {
		t.Fatalf("resource IDs = %#v", inventory.Devices[0].ResourceIDs)
	}
	// A host can only bind to an action it can describe, so the schemas travel
	// with the device.
	for _, capability := range inventory.Devices[0].Capabilities {
		if !json.Valid(capability.InputSchema) {
			t.Fatalf("capability %q published an invalid schema", capability.Name)
		}
	}
}

func TestDriveListCommandRunsTheWholeChain(t *testing.T) {
	dir := populatedDrive(t)
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"drive-list", "--root", "photos=" + dir, "--recursive"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	for _, expected := range []string{"IMG_0001.JPG", "DCIM/100CANON/IMG_0002.JPG", "DCIM/100CANON/notes.txt"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("listing is missing %q: %q", expected, output)
		}
	}

	// A path outside the granted root is refused by the provider, not by the CLI.
	stdout.Reset()
	stderr.Reset()
	if err := Run([]string{"drive-list", "--root", "photos=" + dir, "--path", "../.."}, &stdout, &stderr); err == nil {
		t.Fatal("a path outside the granted root was listed")
	}
}

func TestDriveReadCommandStreamsTheSelectedFile(t *testing.T) {
	dir := populatedDrive(t)
	var stdout, stderr bytes.Buffer
	if err := Run([]string{
		"drive-read", "--root", "photos=" + dir,
		"--path", "DCIM/100CANON/IMG_0002.JPG",
	}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "jpeg2" {
		t.Fatalf("read = %q, want jpeg2", got)
	}
	stdout.Reset()
	if err := Run([]string{
		"drive-read", "--root", "photos=" + dir,
		"--path", "DCIM/100CANON/IMG_0002.JPG", "--max-bytes", "3",
	}, &stdout, &stderr); err == nil || stdout.Len() != 0 {
		t.Fatalf("truncated read returned %q, %v", stdout.String(), err)
	}
	stdout.Reset()
	if err := Run([]string{
		"drive-read", "--root", "photos=" + dir, "--path", "../outside",
	}, &stdout, &stderr); err == nil || stdout.Len() != 0 {
		t.Fatalf("outside read returned %q, %v", stdout.String(), err)
	}
}

func TestDeviceCommandsRejectAmbiguousInvocations(t *testing.T) {
	dir := populatedDrive(t)
	for name, args := range map[string][]string{
		"devices without a root":   {"devices"},
		"devices with a stray arg": {"devices", "--root", "photos=" + dir, "extra"},
		"drive-list with no root":  {"drive-list"},
		"drive-list with two roots": {
			"drive-list", "--root", "a=" + dir, "--root", "b=" + dir,
		},
		"drive-list with a stray arg":    {"drive-list", "--root", "photos=" + dir, "extra"},
		"drive-list with a missing root": {"drive-list", "--root", "photos=" + filepath.Join(dir, "absent")},
		"drive-read with no root":        {"drive-read", "--path", "IMG_0001.JPG"},
		"drive-read with no path":        {"drive-read", "--root", "photos=" + dir},
	} {
		var stdout, stderr bytes.Buffer
		if err := Run(args, &stdout, &stderr); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestUsageListsTheDeviceCommands(t *testing.T) {
	var stderr bytes.Buffer
	if err := Run([]string{"help"}, &bytes.Buffer{}, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"devices", "drive-list", "drive-read", "keyboard-press", "keyboard-type", "--root NAME=PATH", "--pid PID"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("usage is missing %q: %s", expected, stderr.String())
		}
	}
}

func TestKeyboardCommandsRejectIncompleteAndOversizedInput(t *testing.T) {
	for _, args := range [][]string{
		{"keyboard-press"},
		{"keyboard-press", "--pid", "1"},
		{"keyboard-press", "--pid", "1", "--key", "A", "extra"},
		{"keyboard-type"},
		{"keyboard-type", "--pid", "-1"},
	} {
		if err := RunWithInput(args, strings.NewReader("hello"), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid keyboard command %q", args)
		}
	}
	if err := RunWithInput([]string{"keyboard-type", "--pid", "1"}, nil, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("keyboard-type accepted a missing input reader")
	}
	tooLarge := strings.NewReader(strings.Repeat("a", 4*1024+1))
	if err := RunWithInput([]string{"keyboard-type", "--pid", "1"}, tooLarge, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("keyboard-type accepted oversized text")
	}
}
