// Package targetconn resolves opaque target references into ephemeral,
// in-memory adapter connection material.
package targetconn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"cymonkey/src/internal/orchestrator"
)

const MaterialAPIVersion = "interaction.connection/v1alpha1"

type ReferenceKind string

const (
	CredentialReference ReferenceKind = "credential"
	TLSReference        ReferenceKind = "tls"
)

var ErrReferenceNotFound = errors.New("connection material reference not found")
var referencePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)
var headerNamePattern = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)

type Request struct {
	Reference    string
	Kind         ReferenceKind
	TargetID     string
	EndpointName string
	Protocol     string
	Audience     string
}

type Material struct {
	Headers   map[string]string
	TLS       *orchestrator.TLSConnection
	ExpiresAt time.Time
	Release   func(context.Context) error
}

// Resolver resolves an opaque reference. Implementations may delegate to a
// secret manager, an embedding callback, environment configuration, or a
// caller-controlled material directory.
type Resolver interface {
	Resolve(context.Context, Request) (Material, error)
}

type ResolverFunc func(context.Context, Request) (Material, error)

func (f ResolverFunc) Resolve(ctx context.Context, request Request) (Material, error) {
	return f(ctx, request)
}

type Chain []Resolver

func (c Chain) Resolve(ctx context.Context, request Request) (Material, error) {
	for _, resolver := range c {
		if resolver == nil {
			continue
		}
		material, err := resolver.Resolve(ctx, request)
		if err == nil {
			return material, nil
		}
		if !errors.Is(err, ErrReferenceNotFound) {
			return Material{}, err
		}
	}
	return Material{}, ErrReferenceNotFound
}

// EnvironmentResolver reads a strict JSON material document from
// PREFIX_<KIND>_<NORMALIZED_REFERENCE>. Values never enter process arguments.
type EnvironmentResolver struct {
	Prefix string
	Lookup func(string) (string, bool)
}

func (r EnvironmentResolver) Resolve(_ context.Context, request Request) (Material, error) {
	prefix := strings.TrimSpace(r.Prefix)
	if prefix == "" {
		prefix = "JANGOLOVA"
	}
	lookup := r.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	name := prefix + "_" + strings.ToUpper(string(request.Kind)) + "_" + normalizeReference(request.Reference)
	value, ok := lookup(name)
	if !ok {
		return Material{}, ErrReferenceNotFound
	}
	return decodeMaterial(strings.NewReader(value), request.Kind)
}

// DirectoryResolver reads <root>/<kind>/<reference>.json without interpreting
// the reference as a path. The configured root is owned by the caller.
type DirectoryResolver struct{ Root string }

func (r DirectoryResolver) Resolve(_ context.Context, request Request) (Material, error) {
	if !referencePattern.MatchString(request.Reference) {
		return Material{}, errors.New("invalid connection material reference")
	}
	root := strings.TrimSpace(r.Root)
	if root == "" {
		return Material{}, ErrReferenceNotFound
	}
	path := filepath.Join(root, string(request.Kind), request.Reference+".json")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Material{}, ErrReferenceNotFound
	}
	if err != nil {
		return Material{}, errors.New("open connection material document")
	}
	defer file.Close()
	return decodeMaterial(file, request.Kind)
}

// DefaultResolver combines environment documents with an optional material
// directory configured through JANGOLOVA_CONNECTION_MATERIAL_DIR.
func DefaultResolver() Resolver {
	return Chain{
		EnvironmentResolver{},
		DirectoryResolver{Root: os.Getenv("JANGOLOVA_CONNECTION_MATERIAL_DIR")},
	}
}

type materialDocument struct {
	APIVersion string            `json:"apiVersion"`
	Kind       ReferenceKind     `json:"kind"`
	Headers    map[string]string `json:"headers,omitempty"`
	TLS        *tlsDocument      `json:"tls,omitempty"`
	ExpiresAt  time.Time         `json:"expiresAt,omitempty"`
}

type tlsDocument struct {
	CAFile                string `json:"caFile,omitempty"`
	ClientCertificateFile string `json:"clientCertificateFile,omitempty"`
	ClientKeyFile         string `json:"clientKeyFile,omitempty"`
	ServerName            string `json:"serverName,omitempty"`
}

func decodeMaterial(reader io.Reader, kind ReferenceKind) (Material, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, 64*1024+1))
	if err != nil || len(contents) > 64*1024 {
		return Material{}, errors.New("connection material document exceeds 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var document materialDocument
	if err := decoder.Decode(&document); err != nil {
		return Material{}, errors.New("decode connection material document")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Material{}, errors.New("connection material document must contain one JSON value")
	}
	if document.APIVersion != MaterialAPIVersion {
		return Material{}, fmt.Errorf("connection material apiVersion must be %q", MaterialAPIVersion)
	}
	if document.Kind != kind {
		return Material{}, fmt.Errorf("connection material kind must be %q", kind)
	}
	if err := validateHeaders(document.Headers); err != nil {
		return Material{}, err
	}
	if !document.ExpiresAt.IsZero() && !document.ExpiresAt.After(time.Now()) {
		return Material{}, errors.New("connection material is expired")
	}
	material := Material{Headers: cloneHeaders(document.Headers), ExpiresAt: document.ExpiresAt}
	switch kind {
	case CredentialReference:
		if len(material.Headers) == 0 || document.TLS != nil {
			return Material{}, errors.New("credential material must contain headers only")
		}
		if material.ExpiresAt.IsZero() {
			return Material{}, errors.New("credential material expiresAt is required")
		}
	case TLSReference:
		if len(material.Headers) != 0 || document.TLS == nil {
			return Material{}, errors.New("TLS material must contain tls only")
		}
		value, err := validateTLS(*document.TLS)
		if err != nil {
			return Material{}, err
		}
		material.TLS = value
	default:
		return Material{}, errors.New("unsupported connection material reference kind")
	}
	return material, nil
}

func validateHeaders(headers map[string]string) error {
	if len(headers) > 32 {
		return errors.New("connection material has too many headers")
	}
	for name, value := range headers {
		if !headerNamePattern.MatchString(name) || strings.EqualFold(name, "host") || len(name) > 128 {
			return errors.New("connection material contains an invalid header name")
		}
		if len(value) < 8 || len(value) > 16*1024 || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("connection material contains an invalid header value")
		}
	}
	return nil
}

func validateTLS(document tlsDocument) (*orchestrator.TLSConnection, error) {
	paths := []string{document.CAFile, document.ClientCertificateFile, document.ClientKeyFile}
	for _, path := range paths {
		if path != "" && !filepath.IsAbs(path) {
			return nil, errors.New("TLS material file paths must be absolute")
		}
	}
	if (document.ClientCertificateFile == "") != (document.ClientKeyFile == "") {
		return nil, errors.New("TLS client certificate and key files must be supplied together")
	}
	if document.CAFile == "" && document.ClientCertificateFile == "" && document.ServerName == "" {
		return nil, errors.New("TLS material is empty")
	}
	return &orchestrator.TLSConnection{
		CAFile: document.CAFile, ClientCertificateFile: document.ClientCertificateFile,
		ClientKeyFile: document.ClientKeyFile, ServerName: document.ServerName,
	}, nil
}

func normalizeReference(reference string) string {
	var value strings.Builder
	for _, character := range []byte(reference) {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			value.WriteByte(byte(strings.ToUpper(string(character))[0]))
		} else {
			fmt.Fprintf(&value, "_%02X", character)
		}
	}
	return value.String()
}

func cloneHeaders(headers map[string]string) map[string]string {
	values := make(map[string]string, len(headers))
	for name, value := range headers {
		values[http.CanonicalHeaderKey(name)] = value
	}
	return values
}

func wipeHeaders(headers map[string]string) {
	for name, value := range headers {
		buffer := []byte(value)
		for index := range buffer {
			buffer[index] = 0
		}
		headers[name] = string(buffer)
		delete(headers, name)
	}
}

func stableSecretValues(target orchestrator.EngineTarget) []string {
	seen := make(map[string]struct{})
	for _, endpoint := range target.Endpoints {
		if endpoint.Connection == nil {
			continue
		}
		snapshot := endpoint.Connection.Snapshot()
		for _, value := range snapshot.SecretValues {
			if len(value) >= 4 {
				seen[value] = struct{}{}
			}
		}
		for name, value := range snapshot.Headers {
			if len(value) >= 4 {
				seen[value] = struct{}{}
			}
			if strings.EqualFold(name, "authorization") || strings.EqualFold(name, "proxy-authorization") {
				if _, credential, ok := strings.Cut(value, " "); ok && len(credential) >= 4 {
					seen[credential] = struct{}{}
				}
			}
			if strings.EqualFold(name, "cookie") {
				for item := range strings.SplitSeq(value, ";") {
					if _, credential, ok := strings.Cut(strings.TrimSpace(item), "="); ok && len(credential) >= 4 {
						seen[credential] = struct{}{}
					}
				}
			}
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Slice(values, func(left, right int) bool { return len(values[left]) > len(values[right]) })
	return values
}

// Redact removes resolved secret values from an adapter error before it can
// cross a provider or CLI boundary.
func Redact(err error, target orchestrator.EngineTarget) error {
	return RedactContextError(err, target)
}
