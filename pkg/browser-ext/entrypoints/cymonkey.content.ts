import { injectScript } from 'wxt/utils/inject-script';
import { pageCapabilities } from '../src/capabilities';
import { isRecord } from '../src/types';

type OverlayRecord = { host: HTMLElement; shadow: ShadowRoot };
type PageEvent = {
  id: string;
  type: string;
  occurredAt: string;
  domain: 'computer';
  runtime: 'browser-dom';
  driver: 'webextension';
  data: Record<string, unknown>;
};

export default defineContentScript({
  matches: ['<all_urls>'],
  runAt: 'document_start',
  async main() {
    const overlays = new Map<string, OverlayRecord>();
    const events: PageEvent[] = [];
    const allowedPageActions = new Set(pageCapabilities.map((item) => item.name));
    let eventSequence = 0;

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

    browser.runtime.onMessage.addListener((message) => {
      if (!isRecord(message) || message.channel !== 'jangolova.cymonkey.control') return undefined;
      return dispatch(String(message.method || ''), isRecord(message.params) ? message.params : {});
    });

    await injectScript('/cymonkey-main.js', { keepInDom: true });

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
        domains: ['computer'],
        runtimes: ['browser-dom'],
        drivers: ['webextension'],
        features: ['augmentation.page-safe', 'events.cursor', 'overlay.shadow-dom'],
      };
    }

    function describe() {
      return {
        revision: String(eventSequence),
        surfaces: [{
          id: 'document:main', domain: 'computer', runtime: 'browser-dom', kind: 'document',
          label: document.title || undefined, properties: { url: location.href, readyState: document.readyState, overlays: [...overlays.keys()].sort() },
        }],
        augmentations: [],
      };
    }

    function act(name: string, input: Record<string, unknown>) {
      if (name === 'dom.query') return queryDOM(input);
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

    function publishEvent(type: string, data: Record<string, unknown>) {
      eventSequence += 1;
      const event: PageEvent = {
        id: String(eventSequence), type, occurredAt: new Date().toISOString(),
        domain: 'computer', runtime: 'browser-dom', driver: 'webextension', data,
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
