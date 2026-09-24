// Browser-native extension inventory. Packaging and installation run in the
// Jangolova host, since WebExtension APIs cannot install arbitrary ZIPs.
export async function listBrowserExtensions() {
  const api = browser.management;
  if (!api?.getAll) throw new Error('browser extension inventory is unavailable');
  const all = await api.getAll();
  return all.filter((item) => item.type === 'extension').map(description).sort((a, b) => a.id.localeCompare(b.id));
}

export async function describeBrowserExtension(idValue: unknown) {
  const id = requireID(idValue);
  const api = browser.management;
  if (!api?.get) throw new Error('browser extension inventory is unavailable');
  const item = await api.get(id);
  if (item.type !== 'extension') throw new Error('browser item is not an extension');
  return description(item);
}

function requireID(value: unknown) {
  if (typeof value !== 'string' || !/^[A-Za-z0-9@._-]{1,128}$/.test(value)) {
    throw new Error('valid browser extension ID is required');
  }
  return value;
}

function description(item: {
  id: string; name: string; version: string; enabled: boolean; installType: string;
  permissions: string[]; hostPermissions: string[]; mayDisable: boolean;
}) {
  return {
    id: item.id, name: item.name, version: item.version,
    enabled: item.enabled, installType: item.installType,
    permissions: [...item.permissions].sort(),
    hostPermissions: [...item.hostPermissions].sort(),
    mayDisable: item.mayDisable,
  };
}
