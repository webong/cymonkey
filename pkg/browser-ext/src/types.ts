export type CapabilityEffect = 'read' | 'write' | 'external';
export type CymonkeyDomain = 'viewer' | 'render';

export type Capability = {
  name: string;
  description: string;
  effect: CapabilityEffect;
  domain: CymonkeyDomain;
  runtime: 'browser-dom';
  driver: 'webextension';
  support: 'native' | 'mapped' | 'emulated';
  lifetime: 'call' | 'surface' | 'attachment' | 'installation';
  persistence: 'ephemeral' | 'session' | 'persistent';
  inputSchema: {
    type: 'object';
    required: string[];
    additionalProperties: true;
  };
};

export type CymonkeyEvent = {
  id: string;
  type: string;
  occurredAt: string;
  domain: CymonkeyDomain;
  runtime: 'browser-dom';
  driver: 'webextension';
  data: Record<string, unknown>;
};

export type EventQuery = {
  after?: string;
  types?: string[];
  limit?: number;
};

export type EngineRequest = {
  method: string;
  params?: Record<string, unknown>;
};

export type ActionRequest = {
  name: string;
  input?: Record<string, unknown>;
};

export type XalletSpookState = {
  status: 'ready' | 'running' | 'failed';
  xalletSpook: 'discovering' | 'unavailable' | 'connected';
  outboundControl?: 'disabled' | 'connecting' | 'authenticating' | 'connected' | 'unavailable';
  browser: string;
  capabilities: string[];
  lastAction?: string;
  lastError?: string;
  extensionId: string;
};

export function capability(
  name: string,
  description: string,
  effect: CapabilityEffect,
  required: string[],
  lifetime: Capability['lifetime'] = 'installation',
  persistence: Capability['persistence'] = 'persistent',
  domain: CymonkeyDomain = 'viewer',
): Capability {
  return {
    name,
    description,
    effect,
    domain,
    runtime: 'browser-dom',
    driver: 'webextension',
    support: 'native',
    lifetime,
    persistence,
    inputSchema: { type: 'object', required, additionalProperties: true },
  };
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function errorMessage(value: unknown): string {
  return value instanceof Error ? value.message : String(value);
}
