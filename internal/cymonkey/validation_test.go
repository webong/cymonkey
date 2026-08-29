package cymonkey

import (
	"encoding/json"
	"testing"
)

func TestRuntimeAgnosticManifestAcceptsComputerAndRenderTargets(t *testing.T) {
	for name, raw := range map[string]string{
		"browser": `{"apiVersion":"jangolova.cymonkey/v1alpha2","kind":"Augmentation","metadata":{"id":"reading-tools","revision":"1"},"spec":{"targets":[{"domain":"computer","runtime":"browser-dom","match":{"urlPatterns":["https://example.com/*"]}}],"permissions":["dom.query"],"computer":{"scripts":[]}}}`,
		"macos":   `{"apiVersion":"jangolova.cymonkey/v1alpha2","kind":"Augmentation","metadata":{"id":"music-tools","revision":"1"},"spec":{"targets":[{"domain":"computer","runtime":"macos-app","match":{"bundleId":"com.apple.Music"}}],"permissions":["app.command.invoke","ui.query"],"computer":{"commands":[{"id":"play"}]}}}`,
		"render":  `{"apiVersion":"jangolova.cymonkey/v1alpha2","kind":"Augmentation","metadata":{"id":"scene-tools","revision":"1"},"spec":{"targets":[{"domain":"render","runtime":"threejs","match":{"name":"main"}}],"permissions":["camera.projection.set"],"render":{"resources":[{"id":"camera:main","kind":"camera"}]}}}`,
		"player":  `{"apiVersion":"jangolova.cymonkey/v1alpha2","kind":"Augmentation","metadata":{"id":"session-tools","revision":"1"},"spec":{"targets":[{"domain":"player","runtime":"browser-game","match":{"name":"main"}}],"permissions":["player.session.describe"],"player":{"sessions":[{"id":"main"}]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var manifest Manifest
			if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
				t.Fatal(err)
			}
			if err := ValidateManifest(manifest); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimeAgnosticCapabilityRejectsIncompatibleDomainDriver(t *testing.T) {
	err := ValidateCapabilities([]Capability{{
		Name: "ui.query", Domain: DomainComputer, Runtime: "macos-app", Driver: DriverCymonkeyWebSocket,
		Support: SupportMapped, Lifetime: LifetimeAttachment, Persistence: PersistenceSession,
		Effect: "read", InputSchema: json.RawMessage(`{"type":"object"}`),
	}})
	if err == nil {
		t.Fatal("ValidateCapabilities() error = nil")
	}
}

func TestRuntimeAgnosticCapabilityAcceptsContributorDriver(t *testing.T) {
	err := ValidateCapabilities([]Capability{{
		Name: "ui.query", Domain: DomainComputer, Runtime: "example.desktop", Driver: "example-driver",
		Support: SupportNative, Lifetime: LifetimeAttachment, Persistence: PersistenceSession,
		Effect: "read", InputSchema: json.RawMessage(`{"type":"object"}`),
	}})
	if err != nil {
		t.Fatalf("ValidateCapabilities() = %v", err)
	}
}

func TestRuntimeHelloRequiresExactVersionAndKnownDomain(t *testing.T) {
	value := Hello{
		ProtocolVersion: ProtocolVersion, Implementation: Implementation{Name: "fixture"},
		Domains:  []Domain{DomainComputer, DomainRender, DomainPlayer},
		Runtimes: []string{"browser-dom", "threejs", "browser-game"},
		Drivers:  []Driver{DriverWebExtension, DriverInPageRuntime},
	}
	if err := ValidateHello(value); err != nil {
		t.Fatal(err)
	}
	value.ProtocolVersion = "jangolova.cymonkey/v1alpha1"
	if err := ValidateHello(value); err == nil {
		t.Fatal("ValidateHello() accepted a non-standard protocol version")
	}
}
