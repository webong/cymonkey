import { privilegedCapabilities, userscriptCapabilities } from './capabilities';
import { readEvents } from './services/events';
import { changeStyle, executePackagedScripts, registerPackagedScripts, unregisterPackagedScripts } from './services/injection';
import { installOwnedRules, removeOwnedRules } from './services/network';
import { requireScopedIdentifier } from './services/policy';
import { readScopedStorage, writeScopedStorage } from './services/storage';
import { activeTab, requireTabID, sendToTab, targetTab } from './services/tabs';
import { describeRuntime as describeUserscriptRuntime, describeUserscriptManager, dispatchUserscript } from './services/userscripts';
import { isRecord, type EventQuery } from './types';

export async function dispatchCymonkey(method: string, params: Record<string, unknown> = {}) {
  if (method === 'hello') return hello();
  if (method === 'capabilities') return capabilities();
  if (method === 'describe') return describe();
  if (method === 'act') return act(String(params.name || ''), isRecord(params.input) ? params.input : {});
  if (method === 'events') return readEvents(params as EventQuery);
  throw new Error(`unsupported Cymonkey method ${method}`);
}

async function capabilities() {
  const runtime = await describeUserscriptRuntime();
  if (runtime.status === 'available') return privilegedCapabilities;
  const userscriptNames = new Set(userscriptCapabilities.map((item) => item.name));
  return privilegedCapabilities.filter((item) => !userscriptNames.has(item.name));
}

function hello() {
  return {
    protocolVersion: 'jangolova.cymonkey/v1alpha2',
    implementation: {
      name: 'jangolova-browser-extension-webextension',
      version: browser.runtime.getManifest().version,
    },
    drivers: ['webextension'],
    domains: ['computer'],
    runtimes: ['browser-dom'],
    features: [
      'augmentation', 'jangolova.platform-services', 'events.cursor', 'scripts.packaged',
      'userscripts', 'standalone', 'xallet.spook.runtime-discovery',
    ],
  };
}

async function describe() {
  const tab = await activeTab();
  const scripts = await browser.scripting.getRegisteredContentScripts();
  const rules = await browser.declarativeNetRequest.getDynamicRules();
  const page = tab?.id === undefined ? null : await sendToTab(tab.id, 'describe', {}).catch(() => null);
  return {
    revision: `${browser.runtime.getManifest().version}:${scripts.length}:${rules.length}`,
    surfaces: tab ? [{
      id: `web:tab-${tab.id}`,
      domain: 'computer',
      runtime: 'browser-dom',
      kind: 'document',
      label: tab.title || undefined,
      properties: { url: tab.url || null },
    }] : [],
    augmentations: [],
    extension: {
      id: browser.runtime.id,
      product: 'Jangolova Browser Extension',
      version: browser.runtime.getManifest().version,
      distribution: 'single-build',
      browser: import.meta.env.BROWSER,
    },
    activeTab: tab ? { id: tab.id, url: tab.url || null, title: tab.title || null } : null,
    registeredScripts: scripts.map((script) => script.id).sort(),
    userscripts: await describeUserscriptManager(),
    dynamicRuleIds: rules.map((rule) => rule.id).sort((left, right) => left - right),
    page,
  };
}

async function act(name: string, input: Record<string, unknown>) {
  if (name.startsWith('userscript.')) return dispatchUserscript(name, input);
  const augmentationId = name.startsWith('dom.') || name.startsWith('overlay.') ? null : requireAugmentation(input);
  if (name === 'script.execute') return executePackagedScripts(augmentationId!, input);
  if (name === 'script.register') return registerPackagedScripts(augmentationId!, input);
  if (name === 'script.unregister') return unregisterPackagedScripts(augmentationId!, input);
  if (name === 'style.insert') return changeStyle(augmentationId!, input, false);
  if (name === 'style.remove') return changeStyle(augmentationId!, input, true);
  if (name === 'network.rules.install') return installOwnedRules(augmentationId!, input);
  if (name === 'network.rules.remove') return removeOwnedRules(augmentationId!, input);
  if (name === 'storage.get') return readScopedStorage('cymonkey', input);
  if (name === 'storage.set') return writeScopedStorage('cymonkey', input);
  if (['dom.query', 'overlay.mount', 'overlay.patch', 'overlay.unmount'].includes(name)) {
    const tab = await targetTab(input.target);
    return sendToTab(requireTabID(tab), 'act', { name, input });
  }
  throw new Error(`unsupported Cymonkey action ${JSON.stringify(name)}`);
}

function requireAugmentation(input: Record<string, unknown>) {
  return requireScopedIdentifier(input.augmentationId, 'augmentationId');
}
