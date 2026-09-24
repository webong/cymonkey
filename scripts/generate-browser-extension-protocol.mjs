import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';

const root = new URL('../', import.meta.url);
const schemaURL = new URL('src/protocol/browser-extension/v1alpha1/protocol.schema.json', root);
const typescriptURL = new URL('pkg/browser-adapter/src/generated/browser-extension-v1alpha1.ts', root);
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

await output(typescriptURL, typescript);

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
