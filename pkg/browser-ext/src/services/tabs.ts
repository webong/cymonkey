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
  if (!tab?.id) throw new Error('Jangolova has no active target tab');
  return tab;
}

export function requireTabID(tab: { id?: number }) {
  if (!Number.isInteger(tab.id)) throw new Error('target tab has no id');
  return tab.id as number;
}

export async function sendToTab(tabId: number, method: string, params: Record<string, unknown>) {
  return sendToTabChannel(tabId, 'jangolova.cymonkey.control', method, params);
}

export async function sendToTabChannel(tabId: number, channel: string, method: string, params: Record<string, unknown>) {
  return browser.tabs.sendMessage(tabId, { channel, method, params });
}
