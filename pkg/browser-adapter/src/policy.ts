export type PolicyDecision = 'allow' | 'deny';
export type ControlContext = {
  caller: string;
  capability: string;
  effect: 'read' | 'write' | 'external';
  origin?: string;
  tabId?: number;
  owner?: string;
  augmentationId?: string;
};
export type ControlPolicyRule = {
  id: string;
  decision: PolicyDecision;
  callers?: string[];
  capabilities?: string[];
  effects?: ControlContext['effect'][];
  origins?: string[];
  tabIds?: number[];
  owners?: string[];
  augmentationIds?: string[];
};
export type ControlPolicy = {version: 1; defaultDecision: PolicyDecision; rules: ControlPolicyRule[]};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export function validatePolicy(input: unknown): ControlPolicy {
  if (!record(input) || input.version !== 1 || !decision(input.defaultDecision) || !Array.isArray(input.rules) || input.rules.length > 256) {
    throw new Error('control policy must be a bounded version 1 document');
  }
  const seen = new Set<string>();
  const rules = input.rules.map((raw): ControlPolicyRule => {
    if (!record(raw) || typeof raw.id !== 'string' || !identifier.test(raw.id) || seen.has(raw.id) || !decision(raw.decision)) {
      throw new Error('control policy rule is invalid or repeated');
    }
    seen.add(raw.id);
    return {
      id: raw.id, decision: raw.decision,
      ...optionalPatterns(raw.callers, 'callers'),
      ...optionalPatterns(raw.capabilities, 'capabilities'),
      ...optionalPatterns(raw.origins, 'origins'),
      ...optionalPatterns(raw.owners, 'owners'),
      ...optionalPatterns(raw.augmentationIds, 'augmentationIds'),
      ...optionalEffects(raw.effects),
      ...optionalTabIds(raw.tabIds),
    };
  });
  return {version: 1, defaultDecision: input.defaultDecision, rules};
}

export function decidePolicy(policy: ControlPolicy, context: ControlContext): {decision: PolicyDecision; ruleId: string | null} {
  const validated = validatePolicy(policy);
  if (!context || typeof context.caller !== 'string' || typeof context.capability !== 'string' || !['read', 'write', 'external'].includes(context.effect)) {
    throw new Error('control context is invalid');
  }
  const matching = validated.rules.filter((rule) =>
    includes(rule.callers, context.caller) && patterns(rule.capabilities, context.capability)
    && includes(rule.effects, context.effect) && patterns(rule.origins, context.origin)
    && includes(rule.tabIds, context.tabId) && patterns(rule.owners, context.owner)
    && patterns(rule.augmentationIds, context.augmentationId));
  const selected = matching.find((rule) => rule.decision === 'deny') ?? matching.find((rule) => rule.decision === 'allow');
  return {decision: selected?.decision ?? validated.defaultDecision, ruleId: selected?.id ?? null};
}

function optionalPatterns(value: unknown, field: 'callers' | 'capabilities' | 'origins' | 'owners' | 'augmentationIds') {
  if (value === undefined) return {};
  if (!Array.isArray(value) || value.length > 256 || value.some((item) => typeof item !== 'string' || item.length > 512 || !/^[A-Za-z0-9*._:/?=-]+$/.test(item))) {
    throw new Error(`policy ${field} contains an invalid pattern`);
  }
  if (field === 'origins' && value.some((item) => item !== '*' && !/^https?:\/\//.test(item))) throw new Error('policy origins must be HTTP(S) origins or *');
  return {[field]: [...new Set(value)]};
}
function optionalEffects(value: unknown) {
  if (value === undefined) return {};
  if (!Array.isArray(value) || value.some((item) => !['read', 'write', 'external'].includes(item))) throw new Error('policy effects are invalid');
  return {effects: [...new Set(value)]};
}
function optionalTabIds(value: unknown) {
  if (value === undefined) return {};
  if (!Array.isArray(value) || value.some((item) => !Number.isInteger(item) || item < 0)) throw new Error('policy tab ids are invalid');
  return {tabIds: [...new Set(value)]};
}
function decision(value: unknown): value is PolicyDecision { return value === 'allow' || value === 'deny'; }
function includes<T>(values: T[] | undefined, actual: T | undefined) { return values === undefined || (actual !== undefined && values.includes(actual)); }
function patterns(values: string[] | undefined, actual: string | undefined) {
  return values === undefined || (actual !== undefined && values.some((pattern) => pattern === '*' || new RegExp(`^${pattern.split('*').map(escape).join('.*')}$`).test(actual)));
}
function escape(value: string) { return value.replace(/[|\\{}()[\]^$+?.]/g, '\\$&'); }
function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
