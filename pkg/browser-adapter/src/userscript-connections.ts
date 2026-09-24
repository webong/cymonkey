export type UserscriptPort = {
  sender?: {tab?: {id?: number}};
  onMessage: {addListener(listener: (message: unknown) => void): void; removeListener(listener: (message: unknown) => void): void};
  onDisconnect: {addListener(listener: () => void): void; removeListener(listener: () => void): void};
  postMessage(message: unknown): void;
};

export type UserscriptConnectionEvent = {type: 'connected' | 'registered' | 'disconnected' | 'event'; id: string; tabId: number; actions?: string[]; eventType?: string; data?: Record<string, unknown>};

const channel = 'cymonkey.userscript/v1alpha1';
const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const validId = (value: unknown): value is string => typeof value === 'string' && identifier.test(value);
const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const key = (id: string, tabId: number) => `${tabId}\u0000${id}`;

export function createUserscriptConnectionHub(dependencies: {
  authorize: (scriptId: string, tabId: number, port: UserscriptPort) => boolean;
  onEvent?: (event: UserscriptConnectionEvent) => void | Promise<void>;
  timeoutMs?: number;
}) {
  if (typeof dependencies?.authorize !== 'function') throw new Error('userscript connection authorization is required');
  const timeoutMs = dependencies.timeoutMs ?? 10_000;
  if (!Number.isInteger(timeoutMs) || timeoutMs < 1) throw new Error('userscript connection timeout is invalid');
  type Pending = {resolve(value: unknown): void; reject(error: Error): void; timer: ReturnType<typeof setTimeout>};
  type Connection = {id: string; tabId: number; port: UserscriptPort; actions: string[]; pending: Map<string, Pending>};
  const connections = new Map<string, Connection>();
  const listeners = new Set<() => void>();
  let sequence = 0;
  const emit = (event: UserscriptConnectionEvent) => {
    try { void Promise.resolve(dependencies.onEvent?.(event)).catch(() => undefined); } catch { /* transport state remains authoritative */ }
  };
  const close = (connection: Connection) => {
    if (connections.get(key(connection.id, connection.tabId)) !== connection) return;
    connections.delete(key(connection.id, connection.tabId));
    for (const pending of connection.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new Error('userscript disconnected'));
    }
    connection.pending.clear();
    emit({type: 'disconnected', id: connection.id, tabId: connection.tabId});
  };
  return {
    attach(port: UserscriptPort) {
      const tabId = port?.sender?.tab?.id;
      if (!Number.isInteger(tabId) || (tabId as number) < 0 || !port?.onMessage?.addListener
        || !port?.onMessage?.removeListener || !port?.onDisconnect?.addListener
        || !port?.onDisconnect?.removeListener || !port?.postMessage) throw new Error('userscript port is invalid');
      const targetTabId = tabId as number;
      let connection: Connection | undefined;
      const onMessage = (value: unknown) => {
        if (!record(value) || value.channel !== channel || !validId(value.scriptId)) return;
        if (!connection) {
          if (!dependencies.authorize(value.scriptId, targetTabId, port)) return;
          connection = {id: value.scriptId, tabId: targetTabId, port, actions: [], pending: new Map()};
          const previous = connections.get(key(connection.id, targetTabId));
          if (previous) close(previous);
          connections.set(key(connection.id, targetTabId), connection);
          emit({type: 'connected', id: connection.id, tabId: targetTabId});
        }
        const active = connection;
        if (active.id !== value.scriptId) return;
        if (value.type === 'registered') {
          active.actions = Array.isArray(value.actions) ? [...new Set(value.actions.filter(validId))].sort() : [];
          emit({type: 'registered', id: active.id, tabId: targetTabId, actions: [...active.actions]});
        } else if (value.type === 'event' && validId(value.eventType) && record(value.data)) {
          emit({type: 'event', id: active.id, tabId: targetTabId, eventType: value.eventType, data: value.data});
        } else if (value.type === 'response' && typeof value.requestId === 'string') {
          const pending = active.pending.get(value.requestId);
          if (!pending) return;
          clearTimeout(pending.timer);
          active.pending.delete(value.requestId);
          if (typeof value.error === 'string') pending.reject(new Error(value.error));
          else pending.resolve(value.result);
        }
      };
      const onDisconnect = () => {
        cleanup();
      };
      const cleanup = () => {
        if (connection) close(connection);
        port.onMessage.removeListener(onMessage);
        port.onDisconnect.removeListener(onDisconnect);
        listeners.delete(cleanup);
      };
      port.onMessage.addListener(onMessage);
      port.onDisconnect.addListener(onDisconnect);
      listeners.add(cleanup);
      return cleanup;
    },
    describe(id: string, tabId: number) {
      if (!validId(id) || !Number.isInteger(tabId) || tabId < 0) throw new Error('userscript target is invalid');
      const connection = connections.get(key(id, tabId));
      return connection ? {status: 'connected' as const, id, tabId, actions: [...connection.actions]}
        : {status: 'unavailable' as const, id, tabId, actions: []};
    },
    call(id: string, tabId: number, request: unknown): Promise<unknown> {
      if (!validId(id) || !Number.isInteger(tabId) || tabId < 0) throw new Error('userscript target is invalid');
      if (!record(request) || request.method !== 'act' || !record(request.params) || !validId(request.params.name)) {
        throw new Error('userscript request must be an act call with a valid action name');
      }
      const connection = connections.get(key(id, tabId));
      if (!connection) throw new Error('userscript is not connected in the target tab');
      const requestId = `userscript-call-${++sequence}`;
      const payload = {method: 'act', params: {name: request.params.name, input: record(request.params.input) ? request.params.input : {}}};
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => { connection.pending.delete(requestId); reject(new Error('userscript action timed out')); }, timeoutMs);
        connection.pending.set(requestId, {resolve, reject, timer});
        try { connection.port.postMessage({channel, type: 'call', requestId, request: payload}); }
        catch (error) {
          clearTimeout(timer);
          connection.pending.delete(requestId);
          reject(error instanceof Error ? error : new Error(String(error)));
        }
      });
    },
    dispose() {
      for (const connection of connections.values()) close(connection);
      for (const cleanup of [...listeners]) cleanup();
    },
  };
}
