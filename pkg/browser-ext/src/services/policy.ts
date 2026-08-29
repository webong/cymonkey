import { isRecord } from '../types';
import { privilegedCapabilities } from '../capabilities';
import { activeTab } from './tabs';

export type ControlSource = 'xallet-spook' | 'authenticated-websocket' | 'extension-origin';
export type PolicyDecision = 'allow' | 'deny';

export type ControlPolicyRule = {
  id: string;
  decision: PolicyDecision;
  callers?: ControlSource[];
  capabilities?: string[];
  effects?: Array<'read' | 'write' | 'external'>;
  origins?: string[];
  tabIds?: number[];
  augmentationIds?: string[];
};

export type ControlPolicy = {
  version: 1;
  defaultDecision: PolicyDecision;
  rules: ControlPolicyRule[];
};

export type ControlContext = {
  source: ControlSource;
  capability: string;
  effect: 'read' | 'write' | 'external';
  tabId?: number;
  origin?: string;
  augmentationId?: string;
};

export type AuthorizationResult = ControlContext & {
  decision: PolicyDecision;
  ruleId: string | null;
  policyMode: 'configured' | 'default-deny';
};

const policyStorageKey = 'jangolova.controlPolicy.v1';
const capabilityEffects = new Map(privilegedCapabilities.map((item) => [item.name, item.effect]));
const defaultPolicy: ControlPolicy = {
  version: 1,
  defaultDecision: 'deny',
  rules: [
    {id: 'default-read', decision: 'allow', effects: ['read']},
    {
      id: 'default-bootstrap',
      decision: 'allow',
      callers: ['xallet-spook', 'extension-origin'],
      capabilities: ['policy.*', 'control.websocket.*'],
    },
  ],
};

export class ControlPolicyService {
  async describe() {
    const configured = await this.read();
    return configured ? {...configured, mode: 'configured'} : {...defaultPolicy, mode: 'default-deny'};
  }

  async replace(value: unknown) {
    const policy = validatePolicy(value);
    await browser.storage.local.set({[policyStorageKey]: policy});
    return {ok: true, policy};
  }

  async authorize(source: ControlSource, message: Record<string, unknown>): Promise<AuthorizationResult> {
    const context = await controlContext(source, message);
    const configured = await this.read();
    const effective = configured ?? defaultPolicy;
    const matching = effective.rules.filter((rule) => matchesRule(rule, context));
    const selected = matching.find((rule) => rule.decision === 'deny')
      ?? matching.find((rule) => rule.decision === 'allow');
    return {
      ...context,
      decision: selected?.decision ?? effective.defaultDecision,
      ruleId: selected?.id ?? null,
      policyMode: configured ? 'configured' : 'default-deny',
    };
  }

  private async read(): Promise<ControlPolicy | null> {
    const stored = await browser.storage.local.get(policyStorageKey);
    const value = stored[policyStorageKey];
    return value === undefined ? null : validatePolicy(value);
  }
}

export function isExtensionControlCall(message: unknown): message is Record<string, unknown> {
  return isRecord(message) && message.type === 'JANGOLOVA_EXTENSION_CALL';
}

export function requireScopedIdentifier(value: unknown, name: string) {
  if (typeof value !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value)) {
    throw new Error(`${name} contains unsupported characters`);
  }
  return value;
}

export function validatePolicy(value: unknown): ControlPolicy {
  if (!isRecord(value) || value.version !== 1 || (value.defaultDecision !== 'allow' && value.defaultDecision !== 'deny') || !Array.isArray(value.rules)) {
    throw new Error('control policy must be a version 1 policy document');
  }
  if (value.rules.length > 256) throw new Error('control policy has too many rules');
  const identifiers = new Set<string>();
  const rules = value.rules.map((raw): ControlPolicyRule => {
    if (!isRecord(raw)) throw new Error('control policy rule must be an object');
    const id = requireScopedIdentifier(raw.id, 'policy rule id');
    if (identifiers.has(id)) throw new Error(`duplicate control policy rule ${JSON.stringify(id)}`);
    identifiers.add(id);
    if (raw.decision !== 'allow' && raw.decision !== 'deny') throw new Error(`policy rule ${id} has invalid decision`);
    return compact({
      id,
      decision: raw.decision,
      callers: optionalStrings(raw.callers, ['xallet-spook', 'authenticated-websocket', 'extension-origin'], 'callers') as ControlSource[] | undefined,
      capabilities: optionalPatterns(raw.capabilities, 'capabilities'),
      effects: optionalStrings(raw.effects, ['read', 'write', 'external'], 'effects') as ControlPolicyRule['effects'],
      origins: optionalOrigins(raw.origins),
      tabIds: optionalTabIDs(raw.tabIds),
      augmentationIds: optionalIdentifiers(raw.augmentationIds, 'augmentationIds'),
    });
  });
  return {version: 1, defaultDecision: value.defaultDecision, rules};
}

async function controlContext(source: ControlSource, message: Record<string, unknown>): Promise<ControlContext> {
  const resolved = unwrapCall(message);
  const target = isRecord(resolved.input.target) ? resolved.input.target : isRecord(resolved.params.target) ? resolved.params.target : {};
  let tabId = Number.isInteger(target.tabId) ? Number(target.tabId) : undefined;
  let url: string | undefined;
  if (tabId !== undefined) url = (await browser.tabs.get(tabId)).url;
  else if (resolved.effect !== 'read' && affectsTab(resolved.capability)) {
    const tab = await activeTab();
    tabId = tab?.id;
    url = tab?.url;
  }
  const manifest = isRecord(resolved.input.manifest) ? resolved.input.manifest : {};
  const metadata = isRecord(manifest.metadata) ? manifest.metadata : {};
  const augmentationId = typeof resolved.input.augmentationId === 'string'
    ? resolved.input.augmentationId
    : typeof metadata.id === 'string' ? metadata.id : undefined;
  return compact({
    source,
    capability: resolved.capability,
    effect: resolved.effect,
    tabId,
    origin: safeOrigin(url),
    augmentationId,
  });
}

function unwrapCall(message: Record<string, unknown>) {
  const envelopeType = String(message.type || 'JANGOLOVA_EXTENSION_CALL');
  let method = String(message.method || '');
  let params = isRecord(message.params) ? message.params : {};
  if (envelopeType === 'JANGOLOVA_EXTENSION_CALL' && method === 'cymonkey.call') {
    method = String(params.method || '');
    params = isRecord(params.params) ? params.params : {};
  }
	if (method === 'act') {
		const capability = String(params.name || '');
		const input = isRecord(params.input) ? params.input : {};
		return {capability, effect: capabilityEffects.get(capability) ?? 'external', params, input};
	}
  const effect = method === 'hello' || method === 'capabilities' || method === 'describe' || method === 'events' || method === 'policy.describe'
    ? 'read' as const
    : 'external' as const;
  return {capability: method, effect, params, input: {}};
}

function matchesRule(rule: ControlPolicyRule, context: ControlContext) {
  return matches(rule.callers, context.source)
    && matchesPatterns(rule.capabilities, context.capability)
    && matches(rule.effects, context.effect)
    && matchesPatterns(rule.origins, context.origin)
    && matches(rule.tabIds, context.tabId)
    && matchesPatterns(rule.augmentationIds, context.augmentationId);
}

function matches<T>(expected: T[] | undefined, actual: T | undefined) {
  return expected === undefined || (actual !== undefined && expected.includes(actual));
}

function matchesPatterns(expected: string[] | undefined, actual: string | undefined) {
  return expected === undefined || (actual !== undefined && expected.some((pattern) => wildcardMatch(pattern, actual)));
}

function wildcardMatch(pattern: string, actual: string) {
  if (pattern === '*') return true;
  const expression = pattern.split('*').map(escapeRegularExpression).join('.*');
  return new RegExp(`^${expression}$`).test(actual);
}

function escapeRegularExpression(value: string) {
  return value.replace(/[|\\{}()[\]^$+?.]/g, '\\$&');
}

function affectsTab(capability: string) {
  return /^(dom|overlay|script|style|network|cymonkey|cymonkey-engine)\./.test(capability);
}

function safeOrigin(value?: string) {
  if (!value) return undefined;
  try { return new URL(value).origin; } catch { return undefined; }
}

function optionalStrings(value: unknown, allowed: string[], name: string) {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > 256 || value.some((item) => typeof item !== 'string' || !allowed.includes(item))) {
    throw new Error(`policy ${name} must contain supported values`);
  }
  return [...new Set(value as string[])];
}

function optionalPatterns(value: unknown, name: string) {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > 256 || value.some((item) => typeof item !== 'string' || item.length > 512 || !/^[A-Za-z0-9*._:/?=-]+$/.test(item))) {
    throw new Error(`policy ${name} contains an invalid pattern`);
  }
  return [...new Set(value as string[])];
}

function optionalOrigins(value: unknown) {
  const values = optionalPatterns(value, 'origins');
  if (values?.some((item) => item !== '*' && !/^https?:\/\//.test(item))) throw new Error('policy origins must be HTTP(S) origins or *');
  return values;
}

function optionalTabIDs(value: unknown) {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > 256 || value.some((item) => !Number.isInteger(item) || Number(item) < 0)) {
    throw new Error('policy tabIds must be non-negative integers');
  }
  return [...new Set(value as number[])];
}

function optionalIdentifiers(value: unknown, name: string) {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length > 256) throw new Error(`policy ${name} must be a bounded array`);
  return [...new Set(value.map((item) => requireScopedIdentifier(item, name)))];
}

function compact<T extends Record<string, unknown>>(value: T): T {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined)) as T;
}
