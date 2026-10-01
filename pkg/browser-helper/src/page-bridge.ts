export type PageBridgeWindow = {
  cymonkey?: Record<string, unknown>;
  addEventListener(type: 'message', listener: (event: {source: unknown; data: unknown}) => void): void;
  removeEventListener(type: 'message', listener: (event: {source: unknown; data: unknown}) => void): void;
  postMessage(message: unknown, targetOrigin: string): void;
};

// Install in the page's MAIN world. Every method returns a Promise; the
// receiver must expose page-safe actions only.
export function installPageBridgeClient(target: PageBridgeWindow, timeoutMs = 5000) {
  if (!target || !Number.isInteger(timeoutMs) || timeoutMs < 1) throw new Error('page bridge target or timeout is invalid');
  if (target.cymonkey !== undefined && !record(target.cymonkey)) throw new Error('page bridge root is occupied');
  const root = target.cymonkey ??= {};
  if (root.jangolova) throw new Error('page bridge is already installed');
  let sequence = 0;
  const pending = new Map<string, {resolve(value: unknown): void; reject(error: Error): void; timer: ReturnType<typeof setTimeout>}>();
  const receive = (event: {source: unknown; data: unknown}) => {
    if (event.source !== target || !record(event.data) || event.data.channel !== 'cymonkey.jangolova.response') return;
    const id = String(event.data.id ?? '');
    const waiter = pending.get(id);
    if (!waiter) return;
    pending.delete(id);
    clearTimeout(waiter.timer);
    if (event.data.error) waiter.reject(new Error(String(event.data.error)));
    else waiter.resolve(event.data.result);
  };
  target.addEventListener('message', receive);
  const call = (method: string, params: Record<string, unknown> = {}) => new Promise<unknown>((resolve, reject) => {
    const id = `page-${++sequence}`;
    const timer = setTimeout(() => { pending.delete(id); reject(new Error(`page bridge ${method} timed out`)); }, timeoutMs);
    pending.set(id, {resolve, reject, timer});
    target.postMessage({channel: 'cymonkey.jangolova.request', id, method, params}, '*');
  });
  const api = Object.freeze({
    hello: () => call('hello'),
    capabilities: () => call('capabilities'),
    describe: () => call('describe'),
    act: (name: string, input: Record<string, unknown> = {}) => call('act', {name, input}),
    events: (query: Record<string, unknown> = {}) => call('events', query),
  });
  root.jangolova = api;
  return {
    api,
    dispose() {
      target.removeEventListener('message', receive);
      for (const waiter of pending.values()) { clearTimeout(waiter.timer); waiter.reject(new Error('page bridge was disposed')); }
      pending.clear();
      if (root.jangolova === api) delete root.jangolova;
    },
  };
}

// Install in the isolated content world. A page can call this receiver, so
// it refuses privileged actions even if dispatch knows how to execute them.
export function installPageBridgeReceiver(target: PageBridgeWindow, dependencies: {
  allowedActions: ReadonlySet<string>;
  dispatch(method: string, params: Record<string, unknown>): Promise<unknown> | unknown;
}) {
  if (!target || typeof dependencies?.allowedActions?.has !== 'function' || typeof dependencies.dispatch !== 'function') {
    throw new Error('page bridge receiver dependencies are incomplete');
  }
  const receive = (event: {source: unknown; data: unknown}) => {
    if (event.source !== target || !record(event.data) || event.data.channel !== 'cymonkey.jangolova.request') return;
    const id = event.data.id;
    const method = String(event.data.method ?? '');
    const params = record(event.data.params) ? event.data.params : {};
    const work = async () => {
      if (!['hello', 'capabilities', 'describe', 'act', 'events'].includes(method)) throw new Error('unsupported page bridge method');
      if (method === 'act' && !dependencies.allowedActions.has(String(params.name ?? ''))) throw new Error('page bridge cannot invoke privileged action');
      return dependencies.dispatch(method, params);
    };
    void work().then(
      (result) => target.postMessage({channel: 'cymonkey.jangolova.response', id, result}, '*'),
      (error) => target.postMessage({channel: 'cymonkey.jangolova.response', id, error: error instanceof Error ? error.message : String(error)}, '*'),
    );
  };
  target.addEventListener('message', receive);
  return () => target.removeEventListener('message', receive);
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
