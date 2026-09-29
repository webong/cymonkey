package browserextension

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeSigner(t *testing.T, name, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestChromiumSigningKeepsSourceAndKeySeparate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	source := fixture(t)
	prepared, err := Inspect(source)
	if err != nil {
		t.Fatal(err)
	}
	browser := writeSigner(t, "chrome", `
for arg in "$@"; do
  case "$arg" in --pack-extension=*) extension="${arg#*=}";; esac
done
printf 'Cr24fixture' > "${extension}.crx"
printf 'private key' > "${extension}.pem"
`)
	output := filepath.Join(t.TempDir(), "built.crx")
	result, err := SignChromiumCRX(context.Background(), "chrome", browser, source, output, prepared.Revision, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "signed" || result.SourceRevision != prepared.Revision || result.ArtifactRevision == "" || result.KeyPath != strings.TrimSuffix(output, ".crx")+".pem" {
		t.Fatalf("incorrect CRX result: %+v", result)
	}
	keyInfo, err := os.Stat(result.KeyPath)
	if err != nil || keyInfo.Mode().Perm() != 0600 {
		t.Fatalf("private key mode: %v, %v", keyInfo, err)
	}
	if current, err := Inspect(source); err != nil || current.Revision != prepared.Revision {
		t.Fatalf("source changed: %+v, %v", current, err)
	}
	if _, err := SignChromiumCRX(context.Background(), "chrome", browser, source, output, prepared.Revision, ""); err == nil {
		t.Fatal("overwrote existing CRX")
	}
	if _, err := SignChromiumCRX(context.Background(), "chrome", browser, source, filepath.Join(source, "inside.crx"), prepared.Revision, ""); err == nil {
		t.Fatal("accepted CRX output inside extension source")
	}
}

func TestFirefoxSigningUsesEnvironmentAndReturnsSignedArtifactRevision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	source := fixture(t)
	prepared, err := Inspect(source)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := filepath.Join(t.TempDir(), "fixture.zip")
	if _, err := Package(source, unsigned); err != nil {
		t.Fatal(err)
	}
	toolDir := t.TempDir()
	tool := filepath.Join(toolDir, "web-ext")
	if err := os.WriteFile(tool, []byte(`#!/bin/sh
set -eu
for arg in "$@"; do
  case "$arg" in --artifacts-dir=*) artifacts="${arg#*=}";; esac
done
test "$WEB_EXT_API_KEY" = fixture-key
test "$WEB_EXT_API_SECRET" = fixture-secret
cp "$MOCK_XPI" "$artifacts/signed.xpi"
`), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WEB_EXT_API_KEY", "fixture-key")
	t.Setenv("WEB_EXT_API_SECRET", "fixture-secret")
	t.Setenv("MOCK_XPI", unsigned)
	output := filepath.Join(t.TempDir(), "signed.xpi")
	result, err := SignFirefoxXPI(context.Background(), source, output, prepared.Revision)
	if err != nil {
		t.Fatal(err)
	}
	fromXPI, err := Inspect(output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "signed" || result.ArtifactRevision != fromXPI.Revision || result.SourceRevision != prepared.Revision {
		t.Fatalf("incorrect XPI result: %+v", result)
	}
	t.Setenv("WEB_EXT_API_SECRET", "")
	if _, err := SignFirefoxXPI(context.Background(), source, filepath.Join(t.TempDir(), "missing.xpi"), prepared.Revision); err == nil || !strings.Contains(err.Error(), "WEB_EXT_API_SECRET") {
		t.Fatalf("expected missing credentials error, got %v", err)
	}
}
