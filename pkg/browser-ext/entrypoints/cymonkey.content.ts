import { pageCapabilities } from '../src/capabilities';
import { isRecord } from '../src/types';

type OverlayRecord = { host: HTMLElement; shadow: ShadowRoot };
type SandboxPending = { resolve(value: unknown): void; reject(error: Error): void; timer: number };
type SandboxRecord = {
  augmentationId: string;
  host: HTMLElement;
  iframe: HTMLIFrameElement;
  nonce: string;
  configuration: Record<string, unknown>;
  port: MessagePort | null;
  ready: Promise<void>;
  resolveReady(): void;
  rejectReady(error: Error): void;
  pending: Map<string, SandboxPending>;
  mediaSessions: Set<string>;
  timer: number;
};
type PageEvent = {
  id: string;
  type: string;
  occurredAt: string;
  domain: 'render';
  runtime: 'browser-dom';
  driver: 'webextension';
  data: Record<string, unknown>;
};

export default defineContentScript({
  matches: ['<all_urls>'],
  runAt: 'document_start',
  async main() {
    const overlays = new Map<string, OverlayRecord>();
    const sandboxes = new Map<string, SandboxRecord>();
    const events: PageEvent[] = [];
    const allowedPageActions = new Set(pageCapabilities.map((item) => item.name));
    let eventSequence = 0;
    let sandboxSequence = 0;

    window.addEventListener('message', (event) => {
      if (event.source !== window || event.data?.channel !== 'jangolova.cymonkey.request') return;
      void handlePageRequest(event.data).then(
        (result) => window.postMessage({ channel: 'jangolova.cymonkey.response', id: event.data.id, result }, '*'),
        (error) => window.postMessage({
          channel: 'jangolova.cymonkey.response',
          id: event.data.id,
          error: error instanceof Error ? error.message : String(error),
        }, '*'),
      );
    });

    window.addEventListener('message', (event) => {
      const value = event.data;
      if (!isRecord(value) || !['jangolova.cymonkey.sandbox.ready', 'jangolova.cymonkey.sandbox.connected'].includes(String(value.channel))) return;
      const id = typeof value.id === 'string' ? value.id : '';
      const sandbox = sandboxes.get(id);
      if (!sandbox || event.source !== sandbox.iframe.contentWindow || value.nonce !== sandbox.nonce) return;
      if (value.channel === 'jangolova.cymonkey.sandbox.connected') {
        if (value.status === 'connected') sandbox.resolveReady();
        else sandbox.rejectReady(new Error(typeof value.error === 'string' ? value.error : 'sandbox package failed to connect'));
        return;
      }
      if (sandbox.port) return;
      if (value.status !== 'ready') {
        sandbox.rejectReady(new Error(typeof value.error === 'string' ? value.error : 'sandbox package failed to load'));
        return;
      }
      const channel = new MessageChannel();
      sandbox.port = channel.port1;
      sandbox.port.onmessage = (portEvent) => void receiveSandboxMessage(sandbox, portEvent.data);
      sandbox.iframe.contentWindow?.postMessage({
        channel: 'jangolova.cymonkey.sandbox.connect', nonce: sandbox.nonce,
        context: {augmentationId: sandbox.augmentationId, configuration: sandbox.configuration},
      }, '*', [channel.port2]);
    });

    browser.runtime.onMessage.addListener((message) => {
      if (!isRecord(message)) return undefined;
      if (message.channel === 'jangolova.cymonkey.sandbox') {
        return dispatchSandbox(String(message.method || ''), isRecord(message.params) ? message.params : {});
      }
      if (message.channel !== 'jangolova.cymonkey.control') return undefined;
      return dispatch(String(message.method || ''), isRecord(message.params) ? message.params : {});
    });

    async function handlePageRequest(value: unknown) {
      if (!isRecord(value)) throw new Error('invalid Cymonkey page request');
      const method = String(value.method || '');
      const params = isRecord(value.params) ? value.params : {};
      if (method === 'act' && !allowedPageActions.has(String(params.name || ''))) {
        throw new Error(`Cymonkey page bridge cannot invoke privileged action ${JSON.stringify(params.name)}`);
      }
      return dispatch(method, params);
    }

    async function dispatch(method: string, params: Record<string, unknown>) {
      if (method === 'hello') return hello();
      if (method === 'capabilities') return pageCapabilities;
      if (method === 'describe') return describe();
      if (method === 'act') {
        return act(String(params.name || ''), isRecord(params.input) ? params.input : {});
      }
      if (method === 'events') return readEvents(params);
      throw new Error(`unsupported Cymonkey page method ${method}`);
    }

    function hello() {
      return {
        protocolVersion: 'jangolova.cymonkey/v1alpha2',
        implementation: {
          name: 'jangolova-cymonkey-page',
          version: browser.runtime.getManifest().version,
        },
        domains: ['render'],
        runtimes: ['browser-dom'],
        drivers: ['webextension'],
        features: ['augmentation.page-safe', 'events.cursor', 'overlay.shadow-dom'],
      };
    }

    function describe() {
      return {
        revision: String(eventSequence),
        surfaces: [{
          id: 'document:main', domain: 'render', runtime: 'browser-dom', kind: 'document',
          label: document.title || undefined, properties: { url: location.href, readyState: document.readyState, overlays: [...overlays.keys()].sort() },
        }],
        augmentations: [],
      };
    }

    function act(name: string, input: Record<string, unknown>) {
      if (name === 'document.query') return queryDOM(input);
      if (name === 'overlay.mount') return mountOverlay(input, false);
      if (name === 'overlay.patch') return mountOverlay(input, true);
      if (name === 'overlay.unmount') return unmountOverlay(input);
      throw new Error(`unsupported Cymonkey page action ${JSON.stringify(name)}`);
    }

    function queryDOM(input: Record<string, unknown>) {
      const selector = requireString(input.selector, 'selector');
      const limit = Math.min(Math.max(Number(input.limit) || 25, 1), 100);
      const all = [...document.querySelectorAll(selector)];
      const matches = all.slice(0, limit).map((node) => ({
        tag: node.tagName.toLowerCase(),
        id: node.id || null,
        classes: [...node.classList].slice(0, 16),
        text: (node.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 500),
      }));
      return { matches, truncated: all.length > matches.length };
    }

    function mountOverlay(input: Record<string, unknown>, replace: boolean) {
      const id = requireString(input.id, 'id');
      let record = overlays.get(id);
      if (replace && !record) throw new Error(`overlay ${JSON.stringify(id)} does not exist`);
      if (!replace && record) throw new Error(`overlay ${JSON.stringify(id)} already exists`);
      if (!record) {
        const host = document.createElement('div');
        host.dataset.jangolovaCymonkeyOverlay = id;
        const shadow = host.attachShadow({ mode: 'closed' });
        (document.documentElement || document).append(host);
        record = { host, shadow };
        overlays.set(id, record);
      }
      record.shadow.replaceChildren();
      if (typeof input.css === 'string' && input.css) {
        const style = document.createElement('style');
        style.textContent = input.css;
        record.shadow.append(style);
      }
      const surface = document.createElement('div');
      surface.innerHTML = typeof input.html === 'string' ? input.html : '';
      record.shadow.append(surface);
      publishEvent(replace ? 'overlay.patched' : 'overlay.mounted', { id });
      return { ok: true, id };
    }

    function unmountOverlay(input: Record<string, unknown>) {
      const id = requireString(input.id, 'id');
      const record = overlays.get(id);
      if (!record) throw new Error(`overlay ${JSON.stringify(id)} does not exist`);
      record.host.remove();
      overlays.delete(id);
      publishEvent('overlay.unmounted', { id });
      return { ok: true, id };
    }

    async function dispatchSandbox(method: string, input: Record<string, unknown>) {
      if (method === 'mount') return mountSandbox(input);
      if (method === 'unmount') return unmountSandbox(input);
      if (method === 'call') return callSandbox(input);
      throw new Error(`unsupported Cymonkey sandbox method ${JSON.stringify(method)}`);
    }

    async function mountSandbox(input: Record<string, unknown>) {
      const id = requireIdentifier(input.id, 'id');
      const packageId = requireIdentifier(input.package, 'package');
      const augmentationId = requireIdentifier(input.augmentationId, 'augmentationId');
      if (sandboxes.has(id)) throw new Error(`sandbox ${JSON.stringify(id)} already exists`);
      const host = document.createElement('div');
      host.dataset.jangolovaCymonkeySandbox = id;
      const shadow = host.attachShadow({mode: 'closed'});
      const iframe = document.createElement('iframe');
      const nonce = randomNonce();
      const sandboxURL = (browser.runtime.getURL as (path: string) => string)('runtime-sandbox.html');
      iframe.src = `${sandboxURL}?package=${encodeURIComponent(packageId)}&id=${encodeURIComponent(id)}&nonce=${encodeURIComponent(nonce)}`;
      const permissions = sandboxPermissions(input.permissions);
      iframe.allow = permissions.join('; ');
      iframe.title = typeof input.title === 'string' ? input.title.slice(0, 128) : 'Jangolova sandbox';
      iframe.referrerPolicy = 'no-referrer';
      Object.assign(iframe.style, {border: '0', display: 'block', width: '100%', height: '100%'});
      shadow.append(iframe);
      Object.assign(host.style, {
        position: 'fixed', inset: 'auto 24px 24px auto', width: cssLength(input.width, '360px'), height: cssLength(input.height, '640px'),
        zIndex: '2147483647', borderRadius: '12px', overflow: 'hidden', background: '#111', boxShadow: '0 8px 30px rgb(0 0 0 / 35%)',
      });
      const configuration = isRecord(input.configuration) ? input.configuration : {};
      let resolveReady!: () => void;
      let rejectReady!: (error: Error) => void;
      const ready = new Promise<void>((resolve, reject) => { resolveReady = resolve; rejectReady = reject; });
      const sandbox: SandboxRecord = {
        augmentationId, host, iframe, nonce, configuration, port: null, ready, resolveReady, rejectReady,
        pending: new Map(), mediaSessions: new Set(),
        timer: window.setTimeout(() => rejectReady(new Error('sandbox package did not become ready')), 10_000),
      };
      sandboxes.set(id, sandbox);
      (document.documentElement || document).append(host);
      try {
        await ready;
        window.clearTimeout(sandbox.timer);
        publishEvent('sandbox.mounted', {id, package: packageId, augmentationId, permissions});
        return {ok: true, id, package: packageId, permissions};
      } catch (error) {
        removeSandbox(id, sandbox);
        throw error;
      }
    }

    function unmountSandbox(input: Record<string, unknown>) {
      const id = requireIdentifier(input.id, 'id');
      const sandbox = sandboxes.get(id);
      if (!sandbox) throw new Error(`sandbox ${JSON.stringify(id)} does not exist`);
      if (typeof input.augmentationId === 'string' && input.augmentationId !== sandbox.augmentationId) throw new Error('sandbox belongs to a different augmentation');
      removeSandbox(id, sandbox);
      publishEvent('sandbox.unmounted', {id, augmentationId: sandbox.augmentationId});
      return {ok: true, id};
    }

    function callSandbox(input: Record<string, unknown>) {
      const id = requireIdentifier(input.id, 'id');
      const sandbox = sandboxes.get(id);
      if (!sandbox?.port) throw new Error(`sandbox ${JSON.stringify(id)} is not ready`);
      if (input.augmentationId !== sandbox.augmentationId) throw new Error('sandbox belongs to a different augmentation');
      sandboxSequence += 1;
      const requestId = `sandbox-${sandboxSequence}`;
      return new Promise<unknown>((resolve, reject) => {
        const timer = window.setTimeout(() => {
          sandbox.pending.delete(requestId);
          reject(new Error('sandbox Cymonkey request timed out'));
        }, 30_000);
        sandbox.pending.set(requestId, {resolve, reject, timer});
        sandbox.port!.postMessage({channel: 'jangolova.cymonkey.sandbox.request', id: requestId, request: input.request});
      });
    }

    async function receiveSandboxMessage(sandbox: SandboxRecord, value: unknown) {
      if (!isRecord(value) || typeof value.id !== 'string') return;
      if (value.channel === 'jangolova.cymonkey.media.request') {
        await handleSandboxMediaRequest(sandbox, value);
        return;
      }
      if (value.channel !== 'jangolova.cymonkey.sandbox.response') return;
      const pending = sandbox.pending.get(value.id);
      if (!pending) return;
      sandbox.pending.delete(value.id);
      window.clearTimeout(pending.timer);
      if (value.error) pending.reject(new Error(String(value.error))); else pending.resolve(value.result);
    }

    async function handleSandboxMediaRequest(sandbox: SandboxRecord, value: Record<string, unknown>) {
      const requestId = String(value.id);
      const method = String(value.method || '');
      const params = isRecord(value.params) ? value.params : {};
      let sessionId = '';
      try {
        if (method !== 'camera.open' && method !== 'camera.close') throw new Error('sandbox requested an unsupported media operation');
        sessionId = requireIdentifier(params.sessionId, 'media session id');
        if (method === 'camera.open' && sandbox.mediaSessions.has(sessionId)) throw new Error('media session already exists');
        const result = await browser.runtime.sendMessage({
          channel: 'jangolova.media-broker', method,
          params: {...params, sessionId},
        });
        if (method === 'camera.open') sandbox.mediaSessions.add(sessionId);
        else sandbox.mediaSessions.delete(sessionId);
        sandbox.port?.postMessage({channel: 'jangolova.cymonkey.media.response', id: requestId, result});
        publishEvent(method === 'camera.open' ? 'sandbox.media.opened' : 'sandbox.media.closed', {
          augmentationId: sandbox.augmentationId, sessionId,
        });
      } catch (error) {
        sandbox.port?.postMessage({
          channel: 'jangolova.cymonkey.media.response', id: requestId,
          error: error instanceof Error ? error.message : String(error),
        });
      }
    }

    function removeSandbox(id: string, sandbox: SandboxRecord) {
      window.clearTimeout(sandbox.timer);
      sandbox.port?.close();
      for (const pending of sandbox.pending.values()) {
        window.clearTimeout(pending.timer);
        pending.reject(new Error('sandbox was removed'));
      }
      sandbox.pending.clear();
      for (const sessionId of sandbox.mediaSessions) {
        void browser.runtime.sendMessage({
          channel: 'jangolova.media-broker', method: 'camera.close', params: {sessionId},
        }).catch(() => undefined);
      }
      sandbox.mediaSessions.clear();
      sandbox.host.remove();
      sandboxes.delete(id);
    }

    function publishEvent(type: string, data: Record<string, unknown>) {
      eventSequence += 1;
      const event: PageEvent = {
        id: String(eventSequence), type, occurredAt: new Date().toISOString(),
        domain: 'render', runtime: 'browser-dom', driver: 'webextension', data,
      };
      events.push(event);
      if (events.length > 256) events.splice(0, events.length - 256);
      void browser.runtime.sendMessage({ channel: 'jangolova.cymonkey.event', event }).catch(() => undefined);
    }

    function readEvents(query: Record<string, unknown>) {
      const after = Number.parseInt(String(query.after || '0'), 10);
      const types = new Set(Array.isArray(query.types) ? query.types.filter((item): item is string => typeof item === 'string') : []);
      const maximum = Math.min(Math.max(Number(query.limit) || 100, 1), 256);
      return {
        events: events
          .filter((event) => Number(event.id) > after && (types.size === 0 || types.has(event.type)))
          .slice(0, maximum),
        cursor: String(eventSequence),
      };
    }
  },
});

function requireString(value: unknown, name: string) {
  if (typeof value !== 'string' || !value) throw new Error(`${name} is required`);
  return value;
}

function requireIdentifier(value: unknown, name: string) {
  const result = requireString(value, name);
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(result)) throw new Error(`${name} is invalid`);
  return result;
}

function cssLength(value: unknown, fallback: string) {
  return typeof value === 'string' && /^\d{1,4}(?:\.\d{1,2})?(?:px|vw|vh|%)$/.test(value) ? value : fallback;
}

function sandboxPermissions(value: unknown) {
  if (value === undefined) return [];
  if (!Array.isArray(value) || value.some((item) => item !== 'camera')) {
    throw new Error('sandbox permissions currently supports only camera');
  }
  return value.includes('camera') ? ['camera'] : [];
}

function randomNonce() {
  const values = crypto.getRandomValues(new Uint32Array(4));
  return [...values].map((value) => value.toString(16)).join('');
}
