import {decidePolicy, validatePolicy, type ControlContext, type ControlPolicy} from './policy.js';

export type AuditEvent = {
  phase: 'requested' | 'succeeded' | 'denied' | 'failed';
  caller: string;
  capability: string;
  effect: ControlContext['effect'];
  origin?: string;
  tabId?: number;
  owner?: string;
  augmentationId?: string;
  ruleId: string | null;
};

export type ControlGateDependencies = {
  policy?: ControlPolicy;
  audit?: (event: AuditEvent) => Promise<void> | void;
};

// The caller resolves context from authenticated identity and browser-owned
// target data. Never derive it from untrusted page messages alone.
export function createControlGate(dependencies: ControlGateDependencies = {}) {
  let policy = dependencies.policy ? validatePolicy(dependencies.policy) : {version: 1 as const, defaultDecision: 'deny' as const, rules: []};
  const authorize = (context: ControlContext) => decidePolicy(policy, context);
  return {
    describe: () => structuredClone(policy),
    replace(next: unknown) { policy = validatePolicy(next); return structuredClone(policy); },
    authorize,
    async run<T>(context: ControlContext, operation: () => Promise<T>): Promise<T> {
      const decision = authorize(context);
      const base = {
        caller: context.caller, capability: context.capability, effect: context.effect,
        ...(context.origin === undefined ? {} : {origin: context.origin}),
        ...(context.tabId === undefined ? {} : {tabId: context.tabId}),
        ...(context.owner === undefined ? {} : {owner: context.owner}),
        ...(context.augmentationId === undefined ? {} : {augmentationId: context.augmentationId}),
        ruleId: decision.ruleId,
      };
      await dependencies.audit?.({phase: 'requested', ...base});
      if (decision.decision === 'deny') {
        await dependencies.audit?.({phase: 'denied', ...base});
        throw new Error('control policy denied the request');
      }
      try {
        const result = await operation();
        await dependencies.audit?.({phase: 'succeeded', ...base});
        return result;
      } catch (error) {
        await dependencies.audit?.({phase: 'failed', ...base});
        throw error;
      }
    },
  };
}
