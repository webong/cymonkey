export type BrowserTab = {id?: number; url?: string; lastAccessed?: number};
export type TabRouterDependencies = {
  tabs: {
    query(query: Record<string, unknown>): Promise<BrowserTab[]>;
    get(id: number): Promise<BrowserTab>;
    sendMessage(id: number, message: unknown): Promise<unknown>;
  };
  extensionOrigin?: string;
  attach?: (tabId: number) => Promise<void>;
};

export function createTabRouter(dependencies: TabRouterDependencies) {
  if (!dependencies?.tabs?.query || !dependencies.tabs.get || !dependencies.tabs.sendMessage) throw new Error('browser tabs adapter is required');
  const isOwn = (tab: BrowserTab) => Boolean(dependencies.extensionOrigin && tab.url?.startsWith(dependencies.extensionOrigin));
  const active = async () => {
    const focused = await dependencies.tabs.query({active: true, lastFocusedWindow: true});
    const selected = focused.find((tab) => !isOwn(tab));
    if (selected) return selected;
    const all = await dependencies.tabs.query({});
    return all.filter((tab) => !isOwn(tab)).sort((a, b) => (b.lastAccessed ?? 0) - (a.lastAccessed ?? 0))[0] ?? null;
  };
  const resolve = async (target: {tabId?: number} = {}) => {
    const tab = target.tabId === undefined ? await active() : await dependencies.tabs.get(checkTabID(target.tabId));
    if (!tab || !Number.isInteger(tab.id)) throw new Error('no target tab is available');
    return tab as BrowserTab & {id: number};
  };
  return {
    active,
    resolve,
    async send(target: {tabId?: number}, channel: string, method: string, params: Record<string, unknown> = {}) {
      const tab = await resolve(target);
      if (typeof channel !== 'string' || !channel || typeof method !== 'string' || !method) throw new Error('tab message channel and method are required');
      const message = {channel, method, params};
      try { return await dependencies.tabs.sendMessage(tab.id, message); }
      catch (error) {
        if (!dependencies.attach || !missingReceiver(error)) throw error;
        const current = await dependencies.tabs.get(tab.id);
        let protocol = '';
        try { protocol = current.url ? new URL(current.url).protocol : ''; } catch { /* rejected below */ }
        if (protocol !== 'http:' && protocol !== 'https:') throw new Error('tab runtime can attach only to HTTP(S) pages');
        await dependencies.attach(tab.id);
        return dependencies.tabs.sendMessage(tab.id, message);
      }
    },
  };
}

function checkTabID(value: number) {
  if (!Number.isInteger(value) || value < 0) throw new Error('valid tab id is required');
  return value;
}
function missingReceiver(value: unknown) {
  const message = value instanceof Error ? value.message : String(value);
  return /Receiving end does not exist|Could not establish connection|No matching message handler/i.test(message);
}
