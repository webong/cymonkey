// Code generated from protocol/browser-extension/v1alpha1/protocol.schema.json; DO NOT EDIT.
// Schema SHA-256: 700967cedccd9c45dd1492b6bcc10903b9882d8ce47c1b4783544450448f3d24

export const browserExtensionProtocolVersion = 'jangolova.browser-extension/v1alpha1' as const;
export type ExtensionCallType = 'JANGOLOVA_EXTENSION_CALL' | 'CYMONKEY_CALL';
export type ExtensionMethod = "hello" | "capabilities" | "describe" | "events" | "cymonkey.call" | "cymonkey-engine.call" | "policy.describe" | "policy.replace" | "control.websocket.describe" | "control.websocket.configure" | "control.websocket.disable";
export type CymonkeyMethod = "hello" | "capabilities" | "describe" | "act" | "events";
export type ControlMethod = ExtensionMethod | CymonkeyMethod;
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
  type: 'JANGOLOVA_EXTENSION_AUTH';
  protocolVersion: typeof browserExtensionProtocolVersion;
  token: string;
}

export interface ControlResponse {
  type: 'JANGOLOVA_EXTENSION_RESPONSE';
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
