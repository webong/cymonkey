import { isRecord } from '../types';
import { requireScopedIdentifier } from './policy';

export type BrowserPackageDescription = {
  id: string;
  name: string;
  version: string;
  deliveries: Array<{kind: 'augmentation-package' | 'sandbox'; entrypoint: 'content.js' | 'sandbox.js'; browsers: string[]}>;
  permissions: string[];
  capabilities: string[];
  launch?: {name: string; input: Record<string, unknown>};
};

type RegistryEntry = {id: string; manifest: string};

export async function listReviewedPackages(): Promise<BrowserPackageDescription[]> {
  const registry = await loadJSON('augmentations/registry.json');
  if (!isRecord(registry) || registry.apiVersion !== 'jangolova.browser-package/v1alpha1'
    || registry.kind !== 'BrowserPackageRegistry' || !Array.isArray(registry.packages)) {
    throw new Error('browser package registry is invalid');
  }
  const entries = registry.packages.map(validateRegistryEntry);
  const packages = await Promise.all(entries.map(async (entry) => {
    const manifest = await loadJSON(entry.manifest);
    const description = validatePackageManifest(manifest);
    if (description.id !== entry.id) throw new Error(`browser package registry id ${JSON.stringify(entry.id)} does not match its manifest`);
    return description;
  }));
  return packages.sort((left, right) => left.id.localeCompare(right.id));
}

export async function describeReviewedPackage(idValue: unknown) {
  const id = requireScopedIdentifier(idValue, 'package id');
  const packages = await listReviewedPackages();
  const description = packages.find((item) => item.id === id);
  if (!description) throw new Error(`browser package ${JSON.stringify(id)} is not included in this product`);
  return description;
}

export async function requireBrowserPackage(idValue: unknown, requestedPermissions: unknown) {
  const description = await describeReviewedPackage(idValue);
  const delivery = description.deliveries.find((item) => item.browsers.includes(import.meta.env.BROWSER));
  if (!delivery) throw new Error(`browser package ${JSON.stringify(description.id)} does not support ${import.meta.env.BROWSER}`);
  const permissions = validatePermissions(requestedPermissions);
  for (const permission of permissions) {
    if (!description.permissions.includes(permission)) {
      throw new Error(`browser package ${JSON.stringify(description.id)} did not declare permission ${JSON.stringify(permission)}`);
    }
  }
  return {description, delivery, permissions};
}

function validateRegistryEntry(value: unknown): RegistryEntry {
  if (!isRecord(value)) throw new Error('browser package registry entry must be an object');
  const id = requireScopedIdentifier(value.id, 'package id');
  const manifest = `augmentations/${id}/manifest.json`;
  if (value.manifest !== manifest) throw new Error(`browser package ${JSON.stringify(id)} has a non-canonical manifest path`);
  return {id, manifest};
}

function validatePackageManifest(value: unknown): BrowserPackageDescription {
  if (!isRecord(value) || value.apiVersion !== 'jangolova.browser-package/v1alpha1'
    || value.kind !== 'BrowserAugmentationPackage' || !isRecord(value.metadata) || !isRecord(value.spec)) {
    throw new Error('browser package manifest is invalid');
  }
  const id = requireScopedIdentifier(value.metadata.id, 'package id');
  const name = boundedString(value.metadata.name, 'package name', 128);
  const version = boundedString(value.metadata.version, 'package version', 64);
  if (!Array.isArray(value.spec.deliveries) || value.spec.deliveries.length === 0) throw new Error(`browser package ${id} has no delivery`);
  const deliveries = value.spec.deliveries.map((delivery) => {
    if (!isRecord(delivery) || !Array.isArray(delivery.browsers) || delivery.browsers.length === 0) {
      throw new Error(`browser package ${id} has an invalid delivery`);
    }
    const kind = delivery.kind;
    const entrypoint = delivery.entrypoint;
    const supported = kind === 'sandbox' ? ['chrome', 'edge'] : ['chrome', 'edge', 'firefox', 'safari'];
    if ((kind !== 'sandbox' && kind !== 'augmentation-package')
      || (kind === 'sandbox' ? entrypoint !== 'sandbox.js' : entrypoint !== 'content.js')
      || delivery.browsers.some((browserName) => typeof browserName !== 'string' || !supported.includes(browserName))) {
      throw new Error(`browser package ${id} has an invalid ${String(kind)} delivery`);
    }
    return {kind, entrypoint, browsers: [...new Set(delivery.browsers as string[])]} as BrowserPackageDescription['deliveries'][number];
  });
  const permissions = validatePermissions(value.spec.permissions);
  const capabilities = stringArray(value.spec.capabilities, 'package capabilities', 256);
  const launch = validateLaunch(value.spec.launch, capabilities, id);
  return {id, name, version, deliveries, permissions, capabilities, ...(launch ? {launch} : {})};
}

function validateLaunch(value: unknown, capabilities: string[], packageId: string) {
  if (value === undefined) return undefined;
  if (!isRecord(value) || typeof value.name !== 'string' || !capabilities.includes(value.name)
    || (value.input !== undefined && !isRecord(value.input))) {
    throw new Error(`browser package ${packageId} has an invalid launch action`);
  }
  return {name: value.name, input: isRecord(value.input) ? value.input : {}};
}

function validatePermissions(value: unknown) {
  const permissions = stringArray(value ?? [], 'package permissions', 16);
  if (permissions.some((permission) => permission !== 'camera')) {
    throw new Error('browser package permissions currently support only camera');
  }
  return permissions;
}

function stringArray(value: unknown, name: string, maximum: number) {
  if (!Array.isArray(value) || value.length > maximum || value.some((item) => typeof item !== 'string' || !/^[a-z][a-z0-9.-]{0,127}$/.test(item))) {
    throw new Error(`${name} must be a bounded array of identifiers`);
  }
  return [...new Set(value as string[])];
}

function boundedString(value: unknown, name: string, maximum: number) {
  if (typeof value !== 'string' || !value.trim() || value.length > maximum) throw new Error(`${name} is invalid`);
  return value;
}

async function loadJSON(path: string) {
  const response = await fetch((browser.runtime.getURL as (path: string) => string)(path), {cache: 'no-store'});
  if (!response.ok) throw new Error(`could not load reviewed browser package resource ${JSON.stringify(path)}`);
  return response.json() as Promise<unknown>;
}
