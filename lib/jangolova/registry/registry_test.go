package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverSelectAndPullVerifiesArtifact(t *testing.T) {
	artifact := []byte("reviewed Blender module")
	digest := sha256.Sum256(artifact)
	registry := fmt.Sprintf(`{"schemaVersion":%q,"registryId":"test","modules":[{"id":"render/blender","version":"0.1.0","runtime":"blender","protocolVersion":"cymonkey/v1alpha1","status":"available","platforms":["linux-amd64"],"actions":["render.frame"],"artifacts":[{"platform":"linux-amd64","url":"http://127.0.0.1:0/module.tar.gz","sha256":%q}]}]}`, SchemaVersion, hex.EncodeToString(digest[:]))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/index.json":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(registry))
		case "/module.tar.gz":
			_, _ = writer.Write(artifact)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	registry = strings.Replace(registry, "http://127.0.0.1:0", server.URL, 1)
	discovered, err := Discover(context.Background(), server.URL+"/index.json", DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	module, err := discovered.Select("render/blender", "linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	path, err := Pull(context.Background(), module, "linux-amd64", destination, PullOptions{})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(artifact) {
		t.Fatalf("pulled artifact = %q, want %q", contents, artifact)
	}
	if filepath.Dir(path) != destination {
		t.Fatalf("artifact escaped destination: %s", path)
	}
}

func TestDiscoverRejectsNonLoopbackHTTP(t *testing.T) {
	_, err := Discover(context.Background(), "http://registry.example.test/index.json", DiscoveryOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires HTTPS") {
		t.Fatalf("Discover error = %v, want HTTPS requirement", err)
	}
}

func TestDecodeAcceptsHostOwnedSchemaVersion(t *testing.T) {
	const hostVersion = "example.display-modules/v1alpha1"
	encoded := fmt.Sprintf(`{"schemaVersion":%q,"registryId":"host","modules":[{"id":"render/example","version":"1.0.0","runtime":"example","protocolVersion":"example/v1","status":"available"}]}`, hostVersion)
	value, err := DecodeWithSchemaVersion(strings.NewReader(encoded), hostVersion)
	if err != nil {
		t.Fatal(err)
	}
	if value.SchemaVersion != hostVersion || value.Modules[0].ID != "render/example" {
		t.Fatalf("registry = %#v", value)
	}
	if _, err := Decode(strings.NewReader(encoded)); err == nil {
		t.Fatal("native decoder accepted host-owned schema version")
	}
}

func TestPullRejectsDigestMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("tampered"))
	}))
	defer server.Close()
	module := Module{ID: "render/blender", Version: "0.1.0", Platforms: []string{"linux-amd64"}, Artifacts: []Artifact{{Platform: "linux-amd64", URL: server.URL, SHA256: strings.Repeat("0", 64)}}}
	_, err := Pull(context.Background(), module, "linux-amd64", t.TempDir(), PullOptions{})
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("Pull error = %v, want digest mismatch", err)
	}
}
