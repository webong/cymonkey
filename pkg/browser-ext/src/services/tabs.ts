import { isRecord } from '../types';

export async function activeTab() {
  const tabs = await browser.tabs.query({ active: true, lastFocusedWindow: true });
  const ownOrigin = browser.runtime.getURL('/');
  const active = tabs.find((tab) => !tab.url?.startsWith(ownOrigin));
  if (active) return active;
  const candidates = await browser.tabs.query({});
  return candidates
    .filter((tab) => !tab.url?.startsWith(ownOrigin))
    .sort((left, right) => Number(right.lastAccessed || 0) - Number(left.lastAccessed || 0))[0] || null;
}

export async function targetTab(value: unknown) {
  const target = isRecord(value) ? value : {};
  if (Number.isInteger(target.tabId)) return browser.tabs.get(Number(target.tabId));
  const tab = await activeTab();
  if (!tab?.id) throw new Error('Cymonkey has no active target tab');
  return tab;
}

export function requireTabID(tab: { id?: number }) {
  if (!Number.isInteger(tab.id)) throw new Error('target tab has no id');
  return tab.id as number;
}

export async function sendToTab(tabId: number, method: string, params: Record<string, unknown>) {
  return sendToTabChannel(tabId, 'cymonkey.jangolova.control', method, params);
}

export async function sendToTabChannel(tabId: number, channel: string, method: string, params: Record<string, unknown>) {
  const message = { channel, method, params };
  try {
    return await browser.tabs.sendMessage(tabId, message);
  } catch (error) {
    if (!missingReceiver(error)) throw error;
    await attachTabRuntime(tabId);
    return browser.tabs.sendMessage(tabId, message);
  }
}

async function attachTabRuntime(tabId: number) {
  const tab = await browser.tabs.get(tabId);
  let protocol = '';
  try { protocol = tab.url ? new URL(tab.url).protocol : ''; } catch { /* handled below */ }
  if (protocol !== 'http:' && protocol !== 'https:') {
    throw new Error('Cymonkey can attach only to ordinary HTTP(S) pages; browser settings and extension pages are protected');
  }
  try {
    await browser.scripting.executeScript({
      target: {tabId}, files: ['content-scripts/cymonkey-page.js'], world: 'MAIN',
    } as unknown as Parameters<typeof browser.scripting.executeScript>[0]);
    await browser.scripting.executeScript({
      target: {tabId}, files: ['content-scripts/cymonkey.js'], world: 'ISOLATED',
    } as unknown as Parameters<typeof browser.scripting.executeScript>[0]);
  } catch {
    throw new Error('Cymonkey could not attach to the target page; confirm site access is enabled for this extension');
  }
}

function missingReceiver(error: unknown) {
  const message = error instanceof Error ? error.message : String(error);
  return /Receiving end does not exist|Could not establish connection|No matching message handler/i.test(message);
}
