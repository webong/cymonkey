import { capability } from './types';

export const pageCapabilities = [
  capability('document.query', 'Query bounded DOM summaries.', 'read', ['selector'], 'call', 'ephemeral', 'render'),
  capability('overlay.mount', 'Mount a Cymonkey Shadow DOM overlay.', 'write', ['id'], 'surface', 'ephemeral', 'render'),
  capability('overlay.patch', 'Replace a Cymonkey overlay content.', 'write', ['id'], 'surface', 'ephemeral', 'render'),
  capability('overlay.unmount', 'Remove a Cymonkey overlay.', 'write', ['id'], 'surface', 'ephemeral', 'render'),
];

const extensionCapabilities = [
  ...pageCapabilities,
  capability('script.execute', 'Execute packaged augmentation scripts once.', 'external', ['augmentationId', 'files'], 'call', 'ephemeral', 'render'),
  capability('script.register', 'Register a packaged augmentation content script.', 'external', ['augmentationId', 'script'], 'installation', 'persistent', 'render'),
  capability('script.unregister', 'Unregister an augmentation content script.', 'external', ['augmentationId', 'id'], 'installation', 'persistent', 'render'),
  capability('style.insert', 'Insert CSS in a target tab.', 'write', ['augmentationId', 'css'], 'surface', 'ephemeral', 'render'),
  capability('style.remove', 'Remove previously inserted CSS from a target tab.', 'write', ['augmentationId', 'css'], 'surface', 'ephemeral', 'render'),
  capability('sandbox.mount', 'Mount an approved sandbox package in a target tab.', 'write', ['augmentationId', 'id', 'package'], 'surface', 'ephemeral', 'render'),
  capability('sandbox.unmount', 'Remove a mounted sandbox package from a target tab.', 'write', ['augmentationId', 'id'], 'surface', 'ephemeral', 'render'),
  capability('network.rules.install', 'Install owned declarative network rules.', 'external', ['augmentationId', 'rules']),
  capability('network.rules.remove', 'Remove owned declarative network rules.', 'external', ['augmentationId', 'ruleIds']),
  capability('storage.get', 'Read augmentation-scoped extension storage.', 'read', ['augmentationId', 'keys']),
  capability('storage.set', 'Write augmentation-scoped extension storage.', 'write', ['augmentationId', 'values']),
];

export const userscriptCapabilities = [
  capability('userscript.install', 'Install an explicitly approved userscript manifest.', 'external', ['manifest', 'approved']),
  capability('userscript.update', 'Update an installed userscript after permission checks.', 'external', ['manifest']),
  capability('userscript.uninstall', 'Remove an installed userscript.', 'external', ['id']),
  capability('userscript.enable', 'Enable an installed userscript when native execution is available.', 'external', ['id']),
  capability('userscript.disable', 'Disable an installed userscript.', 'external', ['id']),
  capability('userscript.list', 'List source-free userscript descriptions.', 'read', [], 'call', 'ephemeral'),
  capability('userscript.describe', 'Describe one installed userscript without returning source.', 'read', ['id'], 'call', 'ephemeral'),
];

export const privilegedCapabilities = [...extensionCapabilities, ...userscriptCapabilities];

export const privilegedCapabilityNames = privilegedCapabilities.map((item) => item.name);

export function sandboxPackagesSupported() {
  return import.meta.env.BROWSER === 'chrome' || import.meta.env.BROWSER === 'edge';
}

export function browserPrivilegedCapabilityNames() {
  return sandboxPackagesSupported()
    ? privilegedCapabilityNames
    : privilegedCapabilityNames.filter((name) => !name.startsWith('sandbox.'));
}
