export type PageEvent = {
  id: string;
  type: string;
  occurredAt: string;
  domain: 'render';
  runtime: 'browser-dom';
  driver: string;
  data: Record<string, unknown>;
};

export const pageActionNames = Object.freeze(['document.query', 'overlay.mount', 'overlay.patch', 'overlay.unmount']);

export function createDOMPageRuntime(dependencies: {
  document: Document;
  url: () => string;
  driver: string;
  onEvent?: (event: PageEvent) => void | Promise<void>;
}) {
  if (!dependencies?.document?.querySelectorAll || typeof dependencies.url !== 'function'
    || typeof dependencies.driver !== 'string' || !dependencies.driver) throw new Error('DOM page dependencies are incomplete');
  const doc = dependencies.document;
  const overlays = new Map<string, {host: HTMLElement; shadow: ShadowRoot}>();
  const events: PageEvent[] = [];
  let sequence = 0;
  const publish = (type: string, data: Record<string, unknown>) => {
    const event: PageEvent = {
      id: String(++sequence), type, occurredAt: new Date().toISOString(),
      domain: 'render', runtime: 'browser-dom', driver: dependencies.driver, data,
    };
    events.push(event);
    if (events.length > 256) events.shift();
    try { void Promise.resolve(dependencies.onEvent?.(event)).catch(() => undefined); } catch { /* keep local event state */ }
  };
  const requireId = (value: unknown) => {
    if (typeof value !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value)) throw new Error('overlay id is invalid');
    return value;
  };
  const mount = (input: Record<string, unknown>, replace: boolean) => {
    const id = requireId(input.id);
    let entry = overlays.get(id);
    if (replace && !entry) throw new Error(`overlay ${JSON.stringify(id)} does not exist`);
    if (!replace && entry) throw new Error(`overlay ${JSON.stringify(id)} already exists`);
    if (!entry) {
      const host = doc.createElement('div');
      host.dataset.cymonkeyOverlay = id;
      const shadow = host.attachShadow({mode: 'closed'});
      (doc.documentElement ?? doc).append(host);
      entry = {host, shadow};
      overlays.set(id, entry);
    }
    entry.shadow.replaceChildren();
    if (typeof input.css === 'string' && input.css) {
      const style = doc.createElement('style');
      style.textContent = input.css;
      entry.shadow.append(style);
    }
    const surface = doc.createElement('div');
    surface.innerHTML = typeof input.html === 'string' ? input.html : '';
    entry.shadow.append(surface);
    publish(replace ? 'overlay.patched' : 'overlay.mounted', {id});
    return {ok: true, id};
  };
  return {
    actionNames: pageActionNames,
    describe() {
      return {
        revision: String(sequence),
        surfaces: [{
          id: 'document:main', domain: 'render', runtime: 'browser-dom', kind: 'document',
          label: doc.title || undefined,
          properties: {url: dependencies.url(), readyState: doc.readyState, overlays: [...overlays.keys()].sort()},
        }],
        augmentations: [],
      };
    },
    act(name: string, input: Record<string, unknown> = {}) {
      if (!input || typeof input !== 'object' || Array.isArray(input)) throw new Error('page action input is invalid');
      if (name === 'document.query') {
        if (typeof input.selector !== 'string' || !input.selector) throw new Error('selector is required');
        const limit = Math.min(Math.max(Number(input.limit) || 25, 1), 100);
        const nodes = [...doc.querySelectorAll(input.selector)];
        const matches = nodes.slice(0, limit).map((node) => ({
          tag: node.tagName.toLowerCase(), id: node.id || null,
          classes: [...node.classList].slice(0, 16),
          text: (node.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 500),
        }));
        return {matches, truncated: nodes.length > matches.length};
      }
      if (name === 'overlay.mount') return mount(input, false);
      if (name === 'overlay.patch') return mount(input, true);
      if (name === 'overlay.unmount') {
        const id = requireId(input.id);
        const entry = overlays.get(id);
        if (!entry) throw new Error(`overlay ${JSON.stringify(id)} does not exist`);
        entry.host.remove();
        overlays.delete(id);
        publish('overlay.unmounted', {id});
        return {ok: true, id};
      }
      throw new Error(`unsupported page action ${JSON.stringify(name)}`);
    },
    events(query: {after?: string; types?: string[]; limit?: number} = {}) {
      const after = Number.parseInt(String(query.after || '0'), 10);
      const types = new Set(Array.isArray(query.types) ? query.types.filter((item): item is string => typeof item === 'string') : []);
      const maximum = Math.min(Math.max(Number(query.limit) || 100, 1), 256);
      return {events: events.filter((event) => Number(event.id) > after && (!types.size || types.has(event.type))).slice(0, maximum), cursor: String(sequence)};
    },
    dispose() {
      for (const entry of overlays.values()) entry.host.remove();
      overlays.clear();
    },
  };
}
