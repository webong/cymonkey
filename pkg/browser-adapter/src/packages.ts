export type BrowserPackage = {
  id: string;
  name: string;
  version: string;
  deliveries: Array<{kind: 'augmentation-package' | 'sandbox'; entrypoint: 'content.js' | 'sandbox.js'; browsers: string[]}>;
  permissions: string[];
  capabilities: string[];
  launch?: {name: string; input: Record<string, unknown>};
};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const capability = /^[a-z][a-z0-9.-]{0,127}$/;

// Parse caller-supplied data. The integrating extension decides where to load
// the manifest and which reviewed package is allowed to run.
export function validateBrowserPackage(value: unknown): BrowserPackage {
  if (!record(value) || value.apiVersion !== 'cymonkey.browser-package/v1alpha1' || value.kind !== 'BrowserAugmentationPackage'
    || !record(value.metadata) || !record(value.spec)) throw new Error('browser package manifest is invalid');
  const id = bounded(value.metadata.id, identifier, 'package id');
  const name = bounded(value.metadata.name, /^.{1,128}$/, 'package name');
  const version = bounded(value.metadata.version, /^.{1,64}$/, 'package version');
  if (!Array.isArray(value.spec.deliveries) || !value.spec.deliveries.length) throw new Error('browser package has no delivery');
  const deliveries = value.spec.deliveries.map((item): BrowserPackage['deliveries'][number] => {
    if (!record(item) || !Array.isArray(item.browsers) || !item.browsers.length) throw new Error('browser package delivery is invalid');
    const kind = item.kind;
    const entrypoint = item.entrypoint;
    const supported = kind === 'sandbox' ? ['chrome', 'edge'] : ['chrome', 'edge', 'firefox', 'safari'];
    if ((kind !== 'sandbox' && kind !== 'augmentation-package') || entrypoint !== (kind === 'sandbox' ? 'sandbox.js' : 'content.js')
      || item.browsers.some((browser) => typeof browser !== 'string' || !supported.includes(browser))) throw new Error('browser package delivery is invalid');
    return {kind, entrypoint: entrypoint as 'content.js' | 'sandbox.js', browsers: [...new Set(item.browsers as string[])]};
  });
  const permissions = identifiers(value.spec.permissions ?? [], capability, 'permissions', 16);
  if (permissions.some((permission) => permission !== 'camera')) throw new Error('browser package permission is unsupported');
  const capabilities = identifiers(value.spec.capabilities ?? [], capability, 'capabilities', 256);
  let launch: BrowserPackage['launch'];
  if (value.spec.launch !== undefined) {
    const raw = value.spec.launch;
    if (!record(raw) || typeof raw.name !== 'string' || !capabilities.includes(raw.name) || (raw.input !== undefined && !record(raw.input))) throw new Error('browser package launch is invalid');
    launch = {name: raw.name, input: record(raw.input) ? raw.input : {}};
  }
  return {id, name, version, deliveries, permissions, capabilities, ...(launch ? {launch} : {})};
}

function identifiers(value: unknown, pattern: RegExp, name: string, maximum: number): string[] {
  if (!Array.isArray(value) || value.length > maximum || value.some((item) => typeof item !== 'string' || !pattern.test(item))) throw new Error(`browser package ${name} are invalid`);
  return [...new Set(value as string[])];
}
function bounded(value: unknown, pattern: RegExp, name: string): string {
  if (typeof value !== 'string' || !pattern.test(value) || !value.trim()) throw new Error(`browser ${name} is invalid`);
  return value;
}
function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
