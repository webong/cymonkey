import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';

const root = new URL('../', import.meta.url);
const v1alpha2SchemaURL = new URL('protocol/cymonkey/v1alpha2/protocol.schema.json', root);
const goV1alpha2URL = new URL('internal/cymonkeyprotocol/generated_v1alpha2.go', root);

const schemaSource = await readFile(v1alpha2SchemaURL, 'utf8');
const schema = JSON.parse(schemaSource);
const digest = createHash('sha256').update(schemaSource).digest('hex');

const go = `// Code generated from protocol/cymonkey/v1alpha2/protocol.schema.json; DO NOT EDIT.
// Schema SHA-256: ${digest}

package cymonkeyprotocol

import (
	"context"
	"encoding/json"
	"time"
)

const ProtocolVersion = "jangolova.cymonkey/v1alpha2"

type ProfileName string

const (
	ProfileWeb    ProfileName = "web"
	ProfileMacOS  ProfileName = "macos"
	ProfileEngine ProfileName = "engine"
)

type BackendName string

const (
	BackendCDP                BackendName = "cdp"
	BackendBiDi               BackendName = "bidi"
	BackendSafariMCP          BackendName = "safari-mcp"
	BackendWebExtension       BackendName = "webextension"
	BackendMacOSAppleEvents   BackendName = "macos-apple-events"
	BackendMacOSAccessibility BackendName = "macos-accessibility"
	BackendMacOSCooperative   BackendName = "macos-cooperative"
	BackendEngineGodot        BackendName = "engine-godot"
	BackendEngineUnity        BackendName = "engine-unity"
	BackendEngineUnreal       BackendName = "engine-unreal"
	BackendEngineThreejs      BackendName = "engine-threejs"
)

type SupportMode string

const (
	SupportNative   SupportMode = "native"
	SupportMapped   SupportMode = "mapped"
	SupportEmulated SupportMode = "emulated"
)

type Lifetime string

const (
	LifetimeCall           Lifetime = "call"
	LifetimeDocument       Lifetime = "document"
	LifetimeBrowserSession Lifetime = "browser-session"
	LifetimeProfile        Lifetime = "profile"
)

type Persistence string

const (
	PersistenceEphemeral  Persistence = "ephemeral"
	PersistenceSession    Persistence = "session"
	PersistencePersistent Persistence = "persistent"
)

type Effect string

const (
	EffectRead     Effect = "read"
	EffectWrite    Effect = "write"
	EffectExternal Effect = "external"
)

type Implementation struct {
	Name    string \`json:"name"\`
	Version string \`json:"version,omitempty"\`
}

type Hello struct {
	ProtocolVersion    string         \`json:"protocolVersion"\`
	CompatibleProtocols []string      \`json:"compatibleProtocols,omitempty"\`
	Implementation     Implementation \`json:"implementation"\`
	Profiles           []ProfileName  \`json:"profiles"\`
	Backends           []BackendName  \`json:"backends"\`
	Features           []string       \`json:"features,omitempty"\`
}

type Capability struct {
	Name         string          \`json:"name"\`
	Description  string          \`json:"description,omitempty"\`
	Profile      ProfileName     \`json:"profile"\`
	Backend      BackendName     \`json:"backend"\`
	Support      SupportMode     \`json:"support"\`
	Lifetime     Lifetime        \`json:"lifetime"\`
	Persistence  Persistence     \`json:"persistence"\`
	Effect       Effect          \`json:"effect"\`
	TargetKinds  []string        \`json:"targetKinds,omitempty"\`
	InputSchema  json.RawMessage \`json:"inputSchema"\`
	Alternatives []BackendName   \`json:"alternatives,omitempty"\`
}

type Surface struct {
	ID         string         \`json:"id"\`
	Profile    ProfileName    \`json:"profile"\`
	Kind       string         \`json:"kind"\`
	Label      string         \`json:"label,omitempty"\`
	Properties map[string]any \`json:"properties,omitempty"\`
}

type Augmentation struct {
	ID        string        \`json:"id"\`
	Revision  string        \`json:"revision"\`
	Enabled   bool          \`json:"enabled"\`
	Profiles  []ProfileName \`json:"profiles,omitempty"\`
}

type Description struct {
	Revision      string        \`json:"revision"\`
	Surfaces      []Surface     \`json:"surfaces"\`
	Augmentations []Augmentation \`json:"augmentations"\`
}

type Action struct {
	Name  string          \`json:"name"\`
	Input json.RawMessage \`json:"input"\`
}

type Event struct {
	ID         string        \`json:"id"\`
	Type       string        \`json:"type"\`
	OccurredAt time.Time     \`json:"occurredAt"\`
	Profile    ProfileName   \`json:"profile,omitempty"\`
	Backend    BackendName   \`json:"backend,omitempty"\`
	SurfaceID  string        \`json:"surfaceId,omitempty"\`
	Data       json.RawMessage \`json:"data,omitempty"\`
}

type EventBatch struct {
	Cursor string \`json:"cursor"\`
	Events []Event \`json:"events"\`
}

type CallRequest struct {
	Method string          \`json:"method"\`
	Params json.RawMessage \`json:"params,omitempty"\`
}

type CallResponse struct {
	ProtocolVersion string          \`json:"protocolVersion"\`
	InstanceID      string          \`json:"instanceId,omitempty"\`
	Result          json.RawMessage \`json:"result,omitempty"\`
}

type Transport interface {
	Call(context.Context, CallRequest) (CallResponse, error)
}

type Client struct {
	Transport Transport
}

func (c Client) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	response, err := c.Transport.Call(ctx, CallRequest{Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if len(response.Result) == 0 {
		return json.RawMessage("null"), nil
	}
	return response.Result, nil
}
`;

await output(goV1alpha2URL, go);

async function output(url, expected) {
  if (process.argv.includes('--check')) {
    const actual = await readFile(url, 'utf8').catch(() => '');
    if (actual !== expected) {
      console.error(`${url.pathname} is stale; run npm run generate:cymonkey-protocol`);
      process.exitCode = 1;
    }
    return;
  }
  await writeFile(url, expected);
}
