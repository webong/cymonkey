import {validateBrowserPackage, type BrowserPackage} from './packages.js';

export type BrowserPackageRegistry = {apiVersion: 'cymonkey.browser-package/v1alpha1'; kind: 'BrowserPackageRegistry'; packages: Array<{id: string; manifest: string}>};
export type PackageCatalogDependencies = {
  load: (path: string) => Promise<unknown>;
  browser: 'chrome' | 'edge' | 'firefox' | 'safari';
  root?: string;
};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export function validateBrowserPackageRegistry(value: unknown): BrowserPackageRegistry {
  if (!record(value) || value.apiVersion !== 'cymonkey.browser-package/v1alpha1' || value.kind !== 'BrowserPackageRegistry'
    || !Array.isArray(value.packages) || value.packages.length > 256) throw new Error('browser package registry is invalid');
  const ids = new Set<string>();
  const packages = value.packages.map((entry) => {
    if (!record(entry) || typeof entry.id !== 'string' || !identifier.test(entry.id)
      || entry.manifest !== `augmentations/${entry.id}/manifest.json` || ids.has(entry.id)) throw new Error('browser package registry entry is invalid');
    ids.add(entry.id);
    return {id: entry.id, manifest: entry.manifest as string};
  });
  return {apiVersion: 'cymonkey.browser-package/v1alpha1', kind: 'BrowserPackageRegistry', packages};
}

export function createPackageCatalog(dependencies: PackageCatalogDependencies) {
  if (typeof dependencies?.load !== 'function') throw new Error('package loader is required');
  if (!['chrome', 'edge', 'firefox', 'safari'].includes(dependencies.browser)) throw new Error('supported browser is required');
  const root = dependencies.root ?? '';
  if (root && (!root.endsWith('/') || root.includes('..') || root.startsWith('/'))) throw new Error('package root is invalid');
  const load = (path: string) => dependencies.load(root + path);
  const list = async (): Promise<BrowserPackage[]> => {
    const registry = validateBrowserPackageRegistry(await load('augmentations/registry.json'));
    const packages = await Promise.all(registry.packages.map(async (entry) => {
      const manifest = validateBrowserPackage(await load(entry.manifest));
      if (manifest.id !== entry.id) throw new Error(`browser package ${entry.id} does not match its registry entry`);
      return manifest;
    }));
    return packages.sort((left, right) => left.id.localeCompare(right.id));
  };
  const describe = async (id: string) => {
    if (typeof id !== 'string' || !identifier.test(id)) throw new Error('package id is invalid');
    const item = (await list()).find((entry) => entry.id === id);
    if (!item) throw new Error(`browser package ${id} is not reviewed`);
    return item;
  };
  return {
    list,
    describe,
    async require(id: string, requestedPermissions: string[] = []) {
      const item = await describe(id);
      if (!Array.isArray(requestedPermissions) || requestedPermissions.some((permission) => typeof permission !== 'string' || !item.permissions.includes(permission))) {
        throw new Error('package requested undeclared permission');
      }
      const delivery = item.deliveries.find((entry) => entry.browsers.includes(dependencies.browser));
      if (!delivery) throw new Error(`browser package ${id} has no ${dependencies.browser} delivery`);
      return {description: item, delivery, permissions: [...new Set(requestedPermissions)]};
    },
  };
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
