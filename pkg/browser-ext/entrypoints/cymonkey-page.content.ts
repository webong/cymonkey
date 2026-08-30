type PendingCall = {
  resolve(value: unknown): void;
  reject(error: Error): void;
};

declare global {
  interface Window {
    jangolova?: {
      cymonkey?: Readonly<{
        hello(): Promise<unknown>;
        capabilities(): Promise<unknown>;
        describe(): Promise<unknown>;
        act(name: string, input?: Record<string, unknown>): Promise<unknown>;
        events(query?: Record<string, unknown>): Promise<unknown>;
      }>;
      [key: string]: unknown;
    };
  }
}

export default defineContentScript({
  matches: ['<all_urls>'],
  runAt: 'document_start',
  world: 'MAIN',
  main() {
    if (window.jangolova !== undefined && (
      window.jangolova === null || !['object', 'function'].includes(typeof window.jangolova)
    )) return;

    const root = window.jangolova ??= {};
    if (root.cymonkey) return;
    let requestSequence = 0;
    const pending = new Map<string, PendingCall>();

    window.addEventListener('message', (event) => {
      if (event.source !== window || event.data?.channel !== 'jangolova.cymonkey.response') return;
      const waiter = pending.get(String(event.data.id));
      if (!waiter) return;
      pending.delete(String(event.data.id));
      if (event.data.error) waiter.reject(new Error(String(event.data.error)));
      else waiter.resolve(event.data.result);
    });

    function call(method: string, params: Record<string, unknown> = {}) {
      requestSequence += 1;
      const id = `page-${requestSequence}`;
      return new Promise<unknown>((resolve, reject) => {
        const timer = window.setTimeout(() => {
          pending.delete(id);
          reject(new Error(`Cymonkey page bridge ${method} timed out`));
        }, 5000);
        pending.set(id, {
          resolve: (value) => { clearTimeout(timer); resolve(value); },
          reject: (error) => { clearTimeout(timer); reject(error); },
        });
        window.postMessage({ channel: 'jangolova.cymonkey.request', id, method, params }, '*');
      });
    }

    root.cymonkey = Object.freeze({
      hello: () => call('hello'),
      capabilities: () => call('capabilities'),
      describe: () => call('describe'),
      act: (name: string, input: Record<string, unknown> = {}) => call('act', { name, input }),
      events: (query: Record<string, unknown> = {}) => call('events', query),
    });
  },
});
