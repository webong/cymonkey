package targetconn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cymonkey/src/internal/orchestrator"
)

func TestPrepareResolvesRedactsAndReleasesCredential(t *testing.T) {
	released := 0
	resolver := ResolverFunc(func(_ context.Context, request Request) (Material, error) {
		if request.Reference != "browser-session" || request.TargetID != "browser-42" || request.Protocol != "cdp" {
			t.Fatalf("request = %#v", request)
		}
		return Material{
			Headers:   map[string]string{"authorization": "Bearer highly-sensitive-value"},
			ExpiresAt: time.Now().Add(time.Minute),
			Release:   func(context.Context) error { released++; return nil },
		}, nil
	})
	target, release, err := Prepare(context.Background(), resolver, orchestrator.EngineTarget{
		TargetID: "browser-42", Kind: "browser",
		Endpoints: []orchestrator.TargetEndpoint{{Name: "control", Protocol: "cdp", URL: "wss://browser.example/control", CredentialRef: "browser-session"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection := target.Endpoints[0].Connection
	if connection == nil || connection.Headers["Authorization"] != "Bearer highly-sensitive-value" {
		t.Fatalf("connection = %#v", connection)
	}
	redacted := RedactString("dial rejected Bearer highly-sensitive-value", target)
	if strings.Contains(redacted, "highly-sensitive") || !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("redacted = %q", redacted)
	}
	if err := release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if released != 1 || len(connection.Headers) != 0 {
		t.Fatalf("released = %d, headers = %#v", released, connection.Headers)
	}
}

func TestRedactJSONHandlesEscapedAndAuthorizationValues(t *testing.T) {
	target := orchestrator.EngineTarget{Endpoints: []orchestrator.TargetEndpoint{{
		Connection: &orchestrator.EndpointConnection{Headers: map[string]string{
			"Authorization": `Bearer token-with-"quote`,
		}},
	}}}
	document := RedactJSON(json.RawMessage(`{"whole":"Bearer token-with-\"quote","token":"token-with-\"quote"}`), target)
	if !json.Valid(document) || strings.Contains(string(document), "token-with") || strings.Count(string(document), "[REDACTED]") != 2 {
		t.Fatalf("redacted JSON = %s", document)
	}
}

func TestEnvironmentResolverRequiresExpiringCredential(t *testing.T) {
	resolver := EnvironmentResolver{Lookup: func(name string) (string, bool) {
		if name != "JANGOLOVA_CREDENTIAL_BROWSER_2DSESSION" {
			t.Fatalf("environment name = %q", name)
		}
		return `{"apiVersion":"interaction.connection/v1alpha1","kind":"credential","headers":{"Authorization":"Bearer token"}}`, true
	}}
	_, err := resolver.Resolve(context.Background(), Request{Kind: CredentialReference, Reference: "browser-session"})
	if err == nil || !strings.Contains(err.Error(), "expiresAt") {
		t.Fatalf("error = %v", err)
	}
}

func TestDirectoryResolverUsesReferenceAsIdentifier(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "credential")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	document := fmt.Sprintf(`{"apiVersion":"interaction.connection/v1alpha1","kind":"credential","headers":{"Authorization":"Bearer directory-token"},"expiresAt":%q}`, time.Now().Add(time.Minute).Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(directory, "browser-session.json"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	material, err := (DirectoryResolver{Root: root}).Resolve(context.Background(), Request{Kind: CredentialReference, Reference: "browser-session"})
	if err != nil {
		t.Fatal(err)
	}
	if material.Headers["Authorization"] != "Bearer directory-token" {
		t.Fatalf("headers = %#v", material.Headers)
	}
	if _, err := (DirectoryResolver{Root: root}).Resolve(context.Background(), Request{Kind: CredentialReference, Reference: "../browser-session"}); err == nil {
		t.Fatal("path-like reference was accepted")
	}
}

func TestHTTPClientAppliesHeaderAndEnforcesExpiry(t *testing.T) {
	requests := 0
	var expected atomic.Value
	expected.Store("Bearer remote-session")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") != expected.Load().(string) {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	connection := &orchestrator.EndpointConnection{
		Headers:   map[string]string{"Authorization": "Bearer remote-session"},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	endpoint := orchestrator.TargetEndpoint{URL: server.URL, Connection: connection}
	client, err := HTTPClient(endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	expected.Store("Bearer rotated-session")
	connection.ReplaceCredential(map[string]string{"Authorization": "Bearer rotated-session"}, time.Now().Add(2*time.Minute))
	response, err = client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	connection.ReplaceCredential(map[string]string{"Authorization": "Bearer rotated-session"}, time.Now().Add(-time.Second))
	_, err = client.Get(server.URL)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired request error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestHTTPClientUsesCallerResolvedCertificateAuthority(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	certificate := server.Certificate()
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := orchestrator.TargetEndpoint{
		URL:        server.URL,
		Connection: &orchestrator.EndpointConnection{TLS: &orchestrator.TLSConnection{CAFile: caPath}},
	}
	client, err := HTTPClient(endpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestHTTPClientRotatesCertificateAuthorityAfterSuccessfulRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	firstCA := filepath.Join(t.TempDir(), "first-ca.pem")
	secondCA := filepath.Join(t.TempDir(), "second-ca.pem")
	badCA := filepath.Join(t.TempDir(), "private-rotation-name.pem")
	for path, contents := range map[string][]byte{firstCA: caPEM, secondCA: caPEM, badCA: []byte("not a certificate")} {
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	connection := &orchestrator.EndpointConnection{TLS: &orchestrator.TLSConnection{CAFile: firstCA}}
	client, err := HTTPClient(orchestrator.TargetEndpoint{URL: server.URL, Connection: connection}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	revision := connection.ReplaceTLS(&orchestrator.TLSConnection{CAFile: secondCA}, time.Now().Add(time.Minute))
	response, err = client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	_, acknowledged := connection.Acknowledgements()
	if acknowledged < revision {
		t.Fatalf("acknowledged revision = %d, want at least %d", acknowledged, revision)
	}
	transport := client.Transport.(*materialTransport)
	if transport.activeTLSRevision != connection.Snapshot().TLSRevision {
		t.Fatalf("active TLS revision = %d, snapshot = %#v", transport.activeTLSRevision, connection.Snapshot())
	}

	activeRevision := transport.activeTLSRevision
	connection.ReplaceTLS(&orchestrator.TLSConnection{CAFile: badCA}, time.Now().Add(2*time.Minute))
	_, err = client.Get(server.URL)
	if err == nil || strings.Contains(err.Error(), badCA) {
		t.Fatalf("secret-safe failed rotation error = %v", err)
	}
	if transport.activeTLSRevision != activeRevision {
		t.Fatalf("failed rotation promoted TLS revision %d", transport.activeTLSRevision)
	}
	connection.ReplaceTLS(&orchestrator.TLSConnection{CAFile: secondCA}, time.Now().Add(-time.Second))
	_, err = client.Get(server.URL)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired TLS generation error = %v", err)
	}
	if transport.activeTLSRevision != activeRevision {
		t.Fatalf("expired rotation promoted TLS revision %d", transport.activeTLSRevision)
	}
}

func TestHTTPClientRotatesMutualTLSCertificate(t *testing.T) {
	ca, caKey, caPEM := testCertificateAuthority(t)
	serverCertificate, _, _ := testLeafCertificate(t, ca, caKey, 2, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, []net.IP{net.ParseIP("127.0.0.1")})
	_, firstClientPEM, firstClientKey := testLeafCertificate(t, ca, caKey, 3, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil)
	_, secondClientPEM, secondClientKey := testLeafCertificate(t, ca, caKey, 4, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, nil)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	var observed atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
			t.Error("request did not present a client certificate")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		observed.Store(request.TLS.PeerCertificates[0].SerialNumber.Int64())
		w.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverCertificate},
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots,
	}
	server.StartTLS()
	defer server.Close()
	directory := t.TempDir()
	caPath := writeTestMaterial(t, directory, "ca.pem", caPEM)
	firstCertPath := writeTestMaterial(t, directory, "client-1.pem", firstClientPEM)
	firstKeyPath := writeTestMaterial(t, directory, "client-1-key.pem", firstClientKey)
	secondCertPath := writeTestMaterial(t, directory, "client-2.pem", secondClientPEM)
	secondKeyPath := writeTestMaterial(t, directory, "client-2-key.pem", secondClientKey)
	connection := &orchestrator.EndpointConnection{TLS: &orchestrator.TLSConnection{
		CAFile: caPath, ClientCertificateFile: firstCertPath, ClientKeyFile: firstKeyPath,
	}}
	client, err := HTTPClient(orchestrator.TargetEndpoint{URL: server.URL, Connection: connection}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if observed.Load() != 3 {
		t.Fatalf("initial client certificate serial = %d", observed.Load())
	}
	revision := connection.ReplaceTLS(&orchestrator.TLSConnection{
		CAFile: caPath, ClientCertificateFile: secondCertPath, ClientKeyFile: secondKeyPath,
	}, time.Now().Add(time.Minute))
	response, err = client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if observed.Load() != 4 {
		t.Fatalf("rotated client certificate serial = %d", observed.Load())
	}
	_, acknowledged := connection.Acknowledgements()
	if acknowledged < revision {
		t.Fatalf("acknowledged revision = %d, want at least %d", acknowledged, revision)
	}
}

func TestPrepareRejectsCredentialThatExpiresTooSoon(t *testing.T) {
	released := 0
	resolver := ResolverFunc(func(context.Context, Request) (Material, error) {
		return Material{
			Headers:   map[string]string{"Authorization": "Bearer short-lived"},
			ExpiresAt: time.Now().Add(time.Second),
			Release:   func(context.Context) error { released++; return nil },
		}, nil
	})
	_, _, err := Prepare(context.Background(), resolver, orchestrator.EngineTarget{
		Endpoints: []orchestrator.TargetEndpoint{{Name: "cdp", Protocol: "cdp", CredentialRef: "short"}},
	})
	if err == nil || !strings.Contains(err.Error(), "expires too soon") {
		t.Fatalf("error = %v", err)
	}
	if released != 1 {
		t.Fatalf("release count = %d", released)
	}
}

func TestPrepareRenewsCredentialAndReleasesBothGenerations(t *testing.T) {
	var resolves atomic.Int32
	var releases atomic.Int32
	resolver := ResolverFunc(func(context.Context, Request) (Material, error) {
		generation := resolves.Add(1)
		token := "Bearer first-generation"
		expiresAt := time.Now().Add(10 * time.Second)
		if generation > 1 {
			token = "Bearer second-generation"
			expiresAt = time.Now().Add(time.Hour)
		}
		return Material{
			Headers: map[string]string{"Authorization": token}, ExpiresAt: expiresAt,
			Release: func(context.Context) error { releases.Add(1); return nil },
		}, nil
	})
	target, release, err := PrepareWithOptions(context.Background(), resolver, orchestrator.EngineTarget{
		TargetID: "browser-rotation", Endpoints: []orchestrator.TargetEndpoint{{
			Name: "control", Protocol: "cdp", CredentialRef: "rotating-session",
		}},
	}, RenewalOptions{RenewBefore: 10 * time.Second, RetryInterval: 25 * time.Millisecond, ResolveTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	connection := target.Endpoints[0].Connection
	updates := connection.Updates()
	var revision uint64
	select {
	case revision = <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("credential lease was not renewed")
	}
	snapshot := connection.Snapshot()
	if snapshot.Headers["Authorization"] != "Bearer second-generation" || snapshot.Revision < 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if releases.Load() != 0 {
		t.Fatalf("old generation released before reconnect acknowledgement: %d", releases.Load())
	}
	connection.Acknowledge(revision)
	deadline := time.Now().Add(time.Second)
	for releases.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if releases.Load() != 1 {
		t.Fatalf("old generation was not released after acknowledgement: %d", releases.Load())
	}
	if err := release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 2 || len(connection.Snapshot().Headers) != 0 {
		t.Fatalf("final releases = %d, snapshot = %#v", releases.Load(), connection.Snapshot())
	}
}

func TestPrepareRenewsTLSAndReleasesOldFilesAfterAcknowledgement(t *testing.T) {
	var resolves atomic.Int32
	var releases atomic.Int32
	firstCA := filepath.Join(t.TempDir(), "first-ca.pem")
	secondCA := filepath.Join(t.TempDir(), "second-ca.pem")
	resolver := ResolverFunc(func(_ context.Context, request Request) (Material, error) {
		if request.Kind != TLSReference || request.Reference != "browser-ca" {
			t.Fatalf("request = %#v", request)
		}
		generation := resolves.Add(1)
		path := firstCA
		expiresAt := time.Now().Add(10 * time.Second)
		if generation > 1 {
			path = secondCA
			expiresAt = time.Now().Add(time.Hour)
		}
		return Material{
			TLS: &orchestrator.TLSConnection{CAFile: path}, ExpiresAt: expiresAt,
			Release: func(context.Context) error { releases.Add(1); return nil },
		}, nil
	})
	target, release, err := PrepareWithOptions(context.Background(), resolver, orchestrator.EngineTarget{
		TargetID: "browser-tls-rotation", Endpoints: []orchestrator.TargetEndpoint{{
			Name: "control", Protocol: "webdriver-classic", TLSRef: "browser-ca",
		}},
	}, RenewalOptions{RenewBefore: 10 * time.Second, RetryInterval: 25 * time.Millisecond, ResolveTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	connection := target.Endpoints[0].Connection
	updates := connection.Updates()
	var revision uint64
	select {
	case revision = <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("TLS lease was not renewed")
	}
	snapshot := connection.Snapshot()
	if snapshot.TLS == nil || snapshot.TLS.CAFile != secondCA || snapshot.TLSRevision < 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if releases.Load() != 0 {
		t.Fatalf("old TLS files released before acknowledgement: %d", releases.Load())
	}
	connection.Acknowledge(revision)
	deadline := time.Now().Add(time.Second)
	for releases.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if releases.Load() != 1 {
		t.Fatalf("old TLS files were not released after acknowledgement: %d", releases.Load())
	}
	if err := release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 2 || connection.Snapshot().TLS != nil {
		t.Fatalf("final releases = %d, snapshot = %#v", releases.Load(), connection.Snapshot())
	}
}

func TestFailedTLSRenewalKeepsCurrentGeneration(t *testing.T) {
	var resolves atomic.Int32
	var releases atomic.Int32
	firstCA := filepath.Join(t.TempDir(), "active-ca.pem")
	resolver := ResolverFunc(func(context.Context, Request) (Material, error) {
		if resolves.Add(1) > 1 {
			return Material{}, errors.New("secret manager unavailable for private-ca-reference")
		}
		return Material{
			TLS: &orchestrator.TLSConnection{CAFile: firstCA}, ExpiresAt: time.Now().Add(10 * time.Second),
			Release: func(context.Context) error { releases.Add(1); return nil },
		}, nil
	})
	target, release, err := PrepareWithOptions(context.Background(), resolver, orchestrator.EngineTarget{
		Endpoints: []orchestrator.TargetEndpoint{{Name: "control", Protocol: "cdp", TLSRef: "private-ca"}},
	}, RenewalOptions{RenewBefore: 10 * time.Second, RetryInterval: 25 * time.Millisecond, ResolveTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for resolves.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	snapshot := target.Endpoints[0].Connection.Snapshot()
	if resolves.Load() < 2 || snapshot.TLS == nil || snapshot.TLS.CAFile != firstCA || snapshot.TLSRevision != 1 {
		t.Fatalf("failed renewal changed active generation: resolves=%d snapshot=%#v", resolves.Load(), snapshot)
	}
	if releases.Load() != 0 {
		t.Fatalf("active TLS generation released after failed renewal: %d", releases.Load())
	}
	if err := release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 1 {
		t.Fatalf("release count = %d", releases.Load())
	}
}

func TestChainStopsOnlyForReferenceNotFound(t *testing.T) {
	want := errors.New("secret manager unavailable")
	chain := Chain{
		ResolverFunc(func(context.Context, Request) (Material, error) { return Material{}, ErrReferenceNotFound }),
		ResolverFunc(func(context.Context, Request) (Material, error) { return Material{}, want }),
		ResolverFunc(func(context.Context, Request) (Material, error) {
			t.Fatal("unexpected resolver")
			return Material{}, nil
		}),
	}
	_, err := chain.Resolve(context.Background(), Request{})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func testCertificateAuthority(t *testing.T) (*x509.Certificate, *rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Jangolova test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func testLeafCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, serial int64, usages []x509.ExtKeyUsage, addresses []net.IP) (tls.Certificate, []byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "Jangolova test peer"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: usages, IPAddresses: addresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, certificatePEM, keyPEM
}

func writeTestMaterial(t *testing.T, directory, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
