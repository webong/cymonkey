import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { readFile, writeFile } from 'node:fs/promises';

const root = new URL('../', import.meta.url);
const schemaURL = new URL('src/protocol/browser-extension/v1alpha1/protocol.schema.json', root);
const typescriptURL = new URL('pkg/browser-adapter/src/generated/browser-extension-v1alpha1.ts', root);
const goURL = new URL('src/internal/browserextensionprotocol/generated_v1alpha1.go', root);
const schemaSource = await readFile(schemaURL, 'utf8');
const schema = JSON.parse(schemaSource);
const digest = createHash('sha256').update(schemaSource).digest('hex');
const conditions = schema.$defs.controlCall.allOf;
const extensionMethods = conditions[0].then.properties.method.enum;

const typescript = `// Code generated from src/protocol/browser-extension/v1alpha1/protocol.schema.json; DO NOT EDIT.
// Schema SHA-256: ${digest}

export const browserExtensionProtocolVersion = 'cymonkey.browser-extension/v1alpha1' as const;
export type ExtensionCallType = 'CYMONKEY_EXTENSION_CALL';
export type ExtensionMethod = ${union(extensionMethods)};
export type ControlMethod = ExtensionMethod;
export type ControlCaller = 'xallet-spook' | 'authenticated-websocket' | 'extension-origin';
export type CapabilityEffect = 'read' | 'write' | 'external';
export type PolicyDecision = 'allow' | 'deny';

export interface ControlCall {
  type: ExtensionCallType;
  id?: string | number;
  method: ControlMethod;
  params?: Record<string, unknown>;
}

export interface ControlPolicyRule {
  id: string;
  decision: PolicyDecision;
  callers?: ControlCaller[];
  capabilities?: string[];
  effects?: CapabilityEffect[];
  origins?: string[];
  tabIds?: number[];
  augmentationIds?: string[];
}

export interface ControlPolicy {
  version: 1;
  defaultDecision: PolicyDecision;
  rules: ControlPolicyRule[];
}

export interface OutboundControlConfiguration {
  endpoint: string;
  token: string;
  expiresAt: string;
}

export interface AuthRequest {
  type: 'CYMONKEY_EXTENSION_AUTH';
  protocolVersion: typeof browserExtensionProtocolVersion;
  token: string;
}

export interface ControlResponse {
  type: 'CYMONKEY_EXTENSION_RESPONSE';
  id?: string | number | null;
  ok: boolean;
  result?: unknown;
  error?: string;
}

export interface ControlTransport {
  request(call: ControlCall): Promise<ControlResponse>;
}

export class BrowserExtensionClient {
  constructor(private readonly transport: ControlTransport) {}

  async call<T = unknown>(type: ExtensionCallType, method: ControlMethod, params: Record<string, unknown> = {}): Promise<T> {
    const response = await this.transport.request({type, method, params});
    if (!response.ok) throw new Error(response.error || 'browser-extension control call failed');
    return response.result as T;
  }
}
`;

const go = `// Code generated from src/protocol/browser-extension/v1alpha1/protocol.schema.json; DO NOT EDIT.
// Schema SHA-256: ${digest}

package browserextensionprotocol

import (
	"context"
	"encoding/json"
)

const ProtocolVersion = "cymonkey.browser-extension/v1alpha1"

type CallType string

const (
	CallTypeCymonkey CallType = "CYMONKEY_EXTENSION_CALL"
)

type Method string

const (
${[...new Set(extensionMethods)].map((method) => `\tMethod${goName(method)} Method = ${JSON.stringify(method)}`).join('\n')}
)

type ControlCall struct {
	Type   CallType       \`json:"type"\`
	ID     any            \`json:"id,omitempty"\`
	Method Method         \`json:"method"\`
	Params map[string]any \`json:"params,omitempty"\`
}

type ControlPolicyRule struct {
	ID              string   \`json:"id"\`
	Decision        string   \`json:"decision"\`
	Callers         []string \`json:"callers,omitempty"\`
	Capabilities    []string \`json:"capabilities,omitempty"\`
	Effects         []string \`json:"effects,omitempty"\`
	Origins         []string \`json:"origins,omitempty"\`
	TabIDs          []int    \`json:"tabIds,omitempty"\`
	AugmentationIDs []string \`json:"augmentationIds,omitempty"\`
}

type ControlPolicy struct {
	Version         int                 \`json:"version"\`
	DefaultDecision string              \`json:"defaultDecision"\`
	Rules           []ControlPolicyRule \`json:"rules"\`
}

type OutboundControlConfiguration struct {
	Endpoint  string \`json:"endpoint"\`
	Token     string \`json:"token"\`
	ExpiresAt string \`json:"expiresAt"\`
}

type AuthRequest struct {
	Type            string \`json:"type"\`
	ProtocolVersion string \`json:"protocolVersion"\`
	Token           string \`json:"token"\`
}

type ControlResponse struct {
	Type   string          \`json:"type"\`
	ID     any             \`json:"id,omitempty"\`
	OK     bool            \`json:"ok"\`
	Result json.RawMessage \`json:"result,omitempty"\`
	Error  string          \`json:"error,omitempty"\`
}

type Transport interface {
	Call(context.Context, ControlCall) (ControlResponse, error)
}

type Client struct {
	Transport Transport
}

func (c Client) Call(ctx context.Context, callType CallType, method Method, params map[string]any) (json.RawMessage, error) {
	response, err := c.Transport.Call(ctx, ControlCall{Type: callType, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, &ControlError{Message: response.Error}
	}
	return response.Result, nil
}

type ControlError struct {
	Message string
}

func (e *ControlError) Error() string {
	if e.Message == "" {
		return "browser-extension control call failed"
	}
	return e.Message
}
`;

await output(typescriptURL, typescript);
await output(goURL, execFileSync('gofmt', {input: go, encoding: 'utf8'}));

async function output(url, expected) {
  if (process.argv.includes('--check')) {
    const actual = await readFile(url, 'utf8').catch(() => '');
    if (actual !== expected) {
      console.error(`${url.pathname} is stale; run npm run generate:browser-extension-protocol`);
      process.exitCode = 1;
    }
    return;
  }
  await writeFile(url, expected);
}

function union(values) {
  return values.map((value) => JSON.stringify(value)).join(' | ');
}

function goName(value) {
  return value.split(/[^A-Za-z0-9]+/).filter(Boolean).map((part) => part[0].toUpperCase() + part.slice(1)).join('');
}
