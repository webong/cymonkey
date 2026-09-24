import { browserExtensionCapabilities, privilegedCapabilities, sandboxPackagesSupported, userscriptCapabilities } from './capabilities';
import {describeBrowserExtension, listBrowserExtensions} from './services/browser-extensions';
import {mountReviewedAugmentation, unmountReviewedAugmentation} from './services/augmentations';
import { readEvents } from './services/events';
import { changeStyle, executePackagedScripts, registerPackagedScripts, unregisterPackagedScripts } from './services/injection';
import { installOwnedRules, removeOwnedRules } from './services/network';
import { listReviewedPackages } from './services/packages';
import {requireScopedIdentifier} from './services/policy';
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
  const userscriptNames = new Set(userscriptCapabilities.map((item) => item.name));
  const browserExtensionNames = new Set(browserExtensionCapabilities.map((item) => item.name));
  return privilegedCapabilities.filter((item) =>
    (runtime.status === 'available' || !userscriptNames.has(item.name))
    && (browser.management?.getAll || !browserExtensionNames.has(item.name)));
}

function hello() {
  return {
    protocolVersion: 'cymonkey/v1alpha1',
    implementation: {
      name: 'cymonkey-browser-extension-webextension',
      version: browser.runtime.getManifest().version,
    },
    drivers: ['webextension'],
    domains: ['viewer', 'render'],
    runtimes: ['browser-dom'],
    features: [
      'augmentation', 'cymonkey.platform-services', 'events.cursor', 'scripts.packaged',
      'userscripts', 'standalone', 'xallet.spook.runtime-discovery',
      ...(sandboxPackagesSupported() ? ['sandbox.packages'] : []),
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
      domain: 'render',
      runtime: 'browser-dom',
      kind: 'document',
      label: tab.title || undefined,
      properties: { url: tab.url || null },
    }] : [],
    augmentations: [],
    extension: {
      id: browser.runtime.id,
      product: 'Cymonkey Browser Extension',
      version: browser.runtime.getManifest().version,
      distribution: 'single-build',
      browser: import.meta.env.BROWSER,
    },
    activeTab: tab ? { id: tab.id, url: tab.url || null, title: tab.title || null } : null,
    registeredScripts: scripts.map((script) => script.id).sort(),
    userscripts: await describeUserscriptManager(),
    packages: await listReviewedPackages(),
    dynamicRuleIds: rules.map((rule) => rule.id).sort((left, right) => left - right),
    page,
  };
}

async function act(name: string, input: Record<string, unknown>) {
  if (name.startsWith('userscript.')) return dispatchUserscript(name, input);
  if (name === 'extension.list') return listBrowserExtensions();
  if (name === 'extension.describe') return describeBrowserExtension(input.id);
  const augmentationId = name.startsWith('document.') || name.startsWith('overlay.') ? null : requireAugmentation(input);
  if (name === 'script.execute') return executePackagedScripts(augmentationId!, input);
  if (name === 'script.register') return registerPackagedScripts(augmentationId!, input);
  if (name === 'script.unregister') return unregisterPackagedScripts(augmentationId!, input);
  if (name === 'style.insert') return changeStyle(augmentationId!, input, false);
  if (name === 'style.remove') return changeStyle(augmentationId!, input, true);
  if (name === 'augmentation.mount') return mountReviewedAugmentation(input);
  if (name === 'augmentation.unmount') return unmountReviewedAugmentation(input);
  if (name === 'network.rules.install') return installOwnedRules(augmentationId!, input);
  if (name === 'network.rules.remove') return removeOwnedRules(augmentationId!, input);
  if (name === 'storage.get') return readScopedStorage('cymonkey', input);
  if (name === 'storage.set') return writeScopedStorage('cymonkey', input);
  if (['document.query', 'overlay.mount', 'overlay.patch', 'overlay.unmount'].includes(name)) {
    const tab = await targetTab(input.target);
    return sendToTab(requireTabID(tab), 'act', { name, input });
  }
  throw new Error(`unsupported Cymonkey action ${JSON.stringify(name)}`);
}

function requireAugmentation(input: Record<string, unknown>) {
  return requireScopedIdentifier(input.augmentationId, 'augmentationId');
}
