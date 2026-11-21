import {createPageBridgeClient, createPageBridgeReceiver} from 'ext-web/bridge';

export type PageBridgeWindow = {
  cymonkey?: Record<string, unknown>;
  addEventListener(type: 'message', listener: (event: {source: unknown; data: unknown}) => void): void;
  removeEventListener(type: 'message', listener: (event: {source: unknown; data: unknown}) => void): void;
  postMessage(message: unknown, targetOrigin: string): void;
};

const channel = 'cymonkey.display';
const pageMethods = new Set(['hello', 'capabilities', 'describe', 'act', 'events']);
const record = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

// Install in the page's MAIN world. The page global is deliberately limited
// to Cymonkey's page-safe semantic methods, not browser-extension privileges.
export function installPageBridgeClient(target: PageBridgeWindow, timeoutMs = 5000) {
  if (!target || !Number.isInteger(timeoutMs) || timeoutMs < 1) throw new Error('page bridge target or timeout is invalid');
  if (target.cymonkey !== undefined && !record(target.cymonkey)) throw new Error('page bridge root is occupied');
  const root = target.cymonkey ??= {};
  if (root.display) throw new Error('page bridge is already installed');
  const peer = createPageBridgeClient({target, channel, timeoutMs});
  const api = Object.freeze({
    hello: () => peer.call('hello', {}),
    capabilities: () => peer.call('capabilities', {}),
    describe: () => peer.call('describe', {}),
    act: (name: string, input: Record<string, unknown> = {}) => peer.call('act', {name, input}),
    events: (query: Record<string, unknown> = {}) => peer.call('events', query),
  });
  root.display = api;
  return {
    api,
    dispose() {
      peer.close();
      if (root.display === api) delete root.display;
    },
  };
}

// Install in the isolated content world. The hostile page may call this, so
// Cymonkey authorizes the semantic action after ext validates the envelope.
export function installPageBridgeReceiver(target: PageBridgeWindow, dependencies: {
  allowedActions: ReadonlySet<string>;
  dispatch(method: string, params: Record<string, unknown>): Promise<unknown> | unknown;
}) {
  if (!target || typeof dependencies?.allowedActions?.has !== 'function' || typeof dependencies.dispatch !== 'function') {
    throw new Error('page bridge receiver dependencies are incomplete');
  }
  return createPageBridgeReceiver({
    target,
    channel,
    allowedMethods: pageMethods,
    dispatch(method, value) {
      const params = record(value) ? value : {};
      if (method === 'act' && !dependencies.allowedActions.has(String(params.name ?? ''))) {
        throw new Error('page bridge cannot invoke privileged action');
      }
      return dependencies.dispatch(method, params);
    },
  });
}
