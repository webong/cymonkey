import { publishCymonkeyEvent } from './events';
import { requireTabID, targetTab } from './tabs';
import { isRecord } from '../types';

type UserScriptPort = {
  sender?: {tab?: {id?: number}};
  onMessage: {addListener(listener: (message: unknown) => void): void};
  onDisconnect: {addListener(listener: () => void): void};
  postMessage(message: unknown): void;
};

type ScriptConnection = {
  id: string;
  tabId: number;
  actions: string[];
  port: UserScriptPort;
  pending: Map<string, PendingRequest>;
};

type PendingRequest = {resolve(value: unknown): void; reject(error: Error): void; timer: number};

const channel = 'cymonkey.userscript/v1alpha1';
const connections = new Map<string, ScriptConnection>();
let sequence = 0;

export function startUserscriptRuntime() {
  const runtime = browser.runtime as typeof browser.runtime & {
    onUserScriptConnect?: {addListener(listener: (port: UserScriptPort) => void): void};
  };
  runtime.onUserScriptConnect?.addListener((port) => attach(port));
}

export async function callUserscriptRuntime(input: Record<string, unknown>) {
  const id = requireID(input.id);
  const request = requireRequest(input.request);
  const tabId = requireTabID(await targetTab(input.target));
  const connection = connections.get(key(id, tabId));
  if (!connection) throw new Error(`userscript ${JSON.stringify(id)} is not connected in the target tab`);
  const requestId = `userscript-call-${++sequence}`;
  return new Promise((resolve, reject) => {
    const timer = self.setTimeout(() => {
      connection.pending.delete(requestId);
      reject(new Error('userscript action timed out'));
    }, 10_000);
    connection.pending.set(requestId, {resolve, reject, timer});
    try { connection.port.postMessage({channel, type: 'call', requestId, request}); }
    catch (error) {
      self.clearTimeout(timer);
      connection.pending.delete(requestId);
      reject(error instanceof Error ? error : new Error(String(error)));
    }
  });
}

export async function describeUserscriptRuntime(input: Record<string, unknown>) {
  const id = requireID(input.id);
  const tabId = requireTabID(await targetTab(input.target));
  const connection = connections.get(key(id, tabId));
  return connection
    ? {status: 'connected', id, tabId, actions: [...connection.actions]}
    : {status: 'unavailable', id, tabId, actions: []};
}

function attach(port: UserScriptPort) {
  const tabId = port.sender?.tab?.id;
  if (tabId === undefined || !Number.isInteger(tabId)) return;
  let connection: ScriptConnection | undefined;
  port.onMessage.addListener((value) => {
    if (!isRecord(value) || value.channel !== channel || typeof value.scriptId !== 'string' || !validID(value.scriptId)) return;
    if (!connection) {
      connection = {id: value.scriptId, tabId, actions: [], port, pending: new Map()};
      connections.set(key(connection.id, tabId), connection);
      void publishCymonkeyEvent('userscript.runtime.connected', {id: connection.id, tabId}, tabId);
    }
    const active = connection;
    if (!active || active.id !== value.scriptId) return;
    if (value.type === 'registered') {
      active.actions = validActions(value.actions);
      void publishCymonkeyEvent('userscript.runtime.registered', {id: active.id, tabId, actions: active.actions}, tabId);
    } else if (value.type === 'event' && typeof value.eventType === 'string' && validID(value.eventType) && isRecord(value.data)) {
      void publishCymonkeyEvent(`userscript.${active.id}.${value.eventType}`, {id: active.id, ...value.data}, tabId);
    } else if (value.type === 'response' && typeof value.requestId === 'string') {
      const pending = active.pending.get(value.requestId);
      if (!pending) return;
      self.clearTimeout(pending.timer);
      active.pending.delete(value.requestId);
      if (typeof value.error === 'string') pending.reject(new Error(value.error)); else pending.resolve(value.result);
    }
  });
  port.onDisconnect.addListener(() => {
    const active = connection;
    if (!active || connections.get(key(active.id, tabId)) !== active) return;
    connections.delete(key(active.id, tabId));
    for (const pending of active.pending.values()) {
      self.clearTimeout(pending.timer);
      pending.reject(new Error('userscript disconnected'));
    }
    void publishCymonkeyEvent('userscript.runtime.disconnected', {id: active.id, tabId}, tabId);
  });
}

function key(id: string, tabId: number) { return `${tabId}\u0000${id}`; }

function requireID(value: unknown) {
  if (!validID(value)) throw new Error('valid userscript id is required');
  return value;
}

function validID(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value);
}

function validActions(value: unknown) {
  return Array.isArray(value) ? [...new Set(value.filter(validID))].sort() : [];
}

function requireRequest(value: unknown) {
  if (!isRecord(value) || value.method !== 'act' || !isRecord(value.params) || !validID(value.params.name)) {
    throw new Error('userscript request must be an act call with a valid action name');
  }
  return {method: 'act', params: {name: value.params.name, input: isRecord(value.params.input) ? value.params.input : {}}};
}
