import {
  createManifest,
  permissionIncrease,
  publicDescription,
  registrationID,
  registrationPlan,
  registrationWorldID,
  sourceRevision,
  validateManifest,
  type BrowserRegistration,
  type UserScriptManifest,
} from '@jangolova/userscript-runtime';
import { publishCymonkeyEvent } from './events';
import { isRecord } from '../types';
import { authorizeUserscriptMutation } from './userscript-approvals';
import { callUserscriptRuntime, describeUserscriptRuntime } from './userscript-runtime';

const storageKey = 'jangolova.userscripts.v1';

type NativeUserScripts = {
  getScripts(filter?: {ids?: string[]}): Promise<Array<{id: string}>>;
  register(scripts: BrowserRegistration[]): Promise<void>;
  update(scripts: BrowserRegistration[]): Promise<void>;
  unregister(filter?: {ids?: string[]}): Promise<void>;
  configureWorld?(properties: {worldId?: string; messaging?: boolean}): Promise<void>;
};

export async function dispatchUserscript(method: string, input: Record<string, unknown>) {
  if (method === 'userscript.prepare') return prepareUserscript(input);
  if (method === 'userscript.call') return callUserscriptRuntime(input);
  if (method === 'userscript.runtime.describe') return describeUserscriptRuntime(input);
  if (method === 'userscript.list') return listUserscripts();
  if (method === 'userscript.describe') return describeUserscript(requireID(input.id));
  if (method === 'userscript.install') return installUserscript(input);
  if (method === 'userscript.update') return updateUserscript(input);
  if (method === 'userscript.uninstall') return uninstallUserscript(requireID(input.id));
  if (method === 'userscript.enable') return setUserscriptEnabled(requireID(input.id), true);
  if (method === 'userscript.disable') return setUserscriptEnabled(requireID(input.id), false);
  throw new Error(`unsupported userscript method ${JSON.stringify(method)}`);
}

export async function describeUserscriptManager() {
  return {
    runtime: await describeRuntime(),
    installed: await listUserscripts(),
  };
}

export async function describeRuntime() {
  const api = nativeAPI();
  if (!api) return {status: 'unavailable', backend: null, reason: 'browser userScripts API is not exposed'};
  try {
    await api.getScripts();
    return {status: 'available', backend: 'webextension-userScripts'};
  } catch {
    return {status: 'unavailable', backend: 'webextension-userScripts', reason: 'browser userScripts permission or user setting is disabled'};
  }
}

export async function reconcileUserscripts() {
  const records = await readRecords();
  await syncNativeCatalog(records);
  const api = nativeAPI();
  if (!api) return describeRuntime();
  try {
    const expected = Object.values(records).filter((value) => value.spec.enabled);
    const expectedIDs = new Set(expected.map((value) => registrationID(value.metadata.id)));
    const registered = await api.getScripts();
    const orphaned = registered.map((value) => value.id).filter((id) => id.startsWith('jg-us-') && !expectedIDs.has(id));
    if (orphaned.length) await api.unregister({ids: orphaned});
    const actual = new Set(registered.map((value) => value.id));
    const missing = expected.filter((value) => !actual.has(registrationID(value.metadata.id)));
    if (missing.length) {
      for (const value of missing) await configureManagedWorld(api, value);
      await api.register(missing.map(registrationPlan));
    }
    return {status: 'available', registered: expectedIDs.size, restored: missing.length, removed: orphaned.length};
  } catch {
    return {status: 'unavailable', reason: 'userscript runtime reconciliation failed'};
  }
}

async function installUserscript(input: Record<string, unknown>) {
  const manifest = await requireManifest(input.manifest);
  const records = await readRecords();
  if (records[manifest.metadata.id]) throw new Error('userscript is already installed');
  const authorization = await authorizeUserscriptMutation('install', manifest, input.approvalId);
  if (!authorization.approved) return {ok: false, status: 'approval-required', approval: authorization.approval};
  if (manifest.spec.enabled) await register(manifest);
  records[manifest.metadata.id] = manifest;
  await writeRecords(records);
  await publishCymonkeyEvent('userscript.installed', eventData(manifest));
  return publicDescription(manifest);
}

async function updateUserscript(input: Record<string, unknown>) {
  const manifest = await requireManifest(input.manifest);
  const records = await readRecords();
  const previous = records[manifest.metadata.id];
  if (!previous) throw new Error('userscript is not installed');
  const increase = permissionIncrease(previous, manifest);
  const authorization = await authorizeUserscriptMutation('update', manifest, input.approvalId);
  if (!authorization.approved) return {ok: false, status: 'approval-required', approval: authorization.approval};
  if (previous.spec.enabled && manifest.spec.enabled) await updateRegistration(manifest);
  else if (previous.spec.enabled) await unregister(manifest.metadata.id);
  else if (manifest.spec.enabled) await register(manifest);
  records[manifest.metadata.id] = manifest;
  await writeRecords(records);
  await publishCymonkeyEvent('userscript.updated', {...eventData(manifest), permissionIncrease: increase});
  return publicDescription(manifest);
}

async function uninstallUserscript(id: string) {
  const records = await readRecords();
  const existing = records[id];
  if (!existing) throw new Error('userscript is not installed');
  if (existing.spec.enabled) await unregister(id);
  delete records[id];
  await writeRecords(records);
  await publishCymonkeyEvent('userscript.uninstalled', {id});
  return {ok: true, id};
}

async function setUserscriptEnabled(id: string, enabled: boolean) {
  const records = await readRecords();
  const existing = records[id];
  if (!existing) throw new Error('userscript is not installed');
  if (existing.spec.enabled === enabled) return publicDescription(existing);
  const next = structuredClone(existing);
  next.spec.enabled = enabled;
  if (enabled) await register(next); else await unregister(id);
  records[id] = next;
  await writeRecords(records);
  await publishCymonkeyEvent(enabled ? 'userscript.enabled' : 'userscript.disabled', eventData(next));
  return publicDescription(next);
}

async function listUserscripts() {
  const records = await readRecords();
  return Object.values(records).sort((left, right) => left.metadata.id.localeCompare(right.metadata.id)).map(publicDescription);
}

async function describeUserscript(id: string) {
  const records = await readRecords();
  const value = records[id];
  if (!value) throw new Error('userscript is not installed');
  return publicDescription(value);
}

async function prepareUserscript(input: Record<string, unknown>) {
  return createManifest({
    id: requireID(input.id),
    name: requireText(input.name, 'userscript name', 128),
    matches: requireStrings(input.matches, 'userscript matches'),
    code: requireText(input.code, 'userscript code', 1024 * 1024),
    ...(input.namespace === undefined ? {} : {namespace: requireText(input.namespace, 'userscript namespace', 512)}),
    ...(input.version === undefined ? {} : {version: requireText(input.version, 'userscript version', 128)}),
    ...(input.description === undefined ? {} : {description: requireText(input.description, 'userscript description', 1024)}),
    ...(input.excludeMatches === undefined ? {} : {excludeMatches: requireStrings(input.excludeMatches, 'userscript exclude matches')}),
    ...(input.runAt === undefined ? {} : {runAt: requireRunAt(input.runAt)}),
    ...(input.world === undefined ? {} : {world: requireWorld(input.world)}),
    ...(input.allFrames === undefined ? {} : {allFrames: requireBoolean(input.allFrames, 'userscript allFrames')}),
    ...(input.enabled === undefined ? {} : {enabled: requireBoolean(input.enabled, 'userscript enabled')}),
    ...(input.updateUrl === undefined ? {} : {updateUrl: requireText(input.updateUrl, 'userscript update URL', 2048)}),
  });
}

async function requireManifest(value: unknown) {
  if (!isRecord(value)) throw new Error('userscript manifest is required');
  // This parses a prospective MAIN-world manifest for review; registration only
  // occurs after the popup has approved this exact revision.
  const manifest = validateManifest(value as unknown as UserScriptManifest, {allowMainWorld: true});
  if (await sourceRevision(manifest.source.code) !== manifest.metadata.revision) throw new Error('userscript source does not match its revision');
  return manifest;
}

async function register(value: UserScriptManifest) {
  const api = await requireNativeAPI();
  await configureManagedWorld(api, value);
  await api.register([registrationPlan(value)]);
}

async function updateRegistration(value: UserScriptManifest) {
  const api = await requireNativeAPI();
  await configureManagedWorld(api, value);
  await api.update([registrationPlan(value)]);
}

async function unregister(id: string) {
  const api = await requireNativeAPI();
  await api.unregister({ids: [registrationID(id)]});
}

async function requireNativeAPI() {
  const api = nativeAPI();
  if (!api) throw new Error('browser userscript runtime is unavailable');
  try { await api.getScripts(); } catch { throw new Error('browser userscript permission or user setting is disabled'); }
  return api;
}

async function configureManagedWorld(api: NativeUserScripts, value: UserScriptManifest) {
  if (value.spec.world !== 'USER_SCRIPT' || !value.source.code.includes('jangolova.cymonkey.userscript/v1alpha1')) return;
  if (!api.configureWorld) return;
  // Messaging is an optional refinement. The reviewed script still runs as a
  // normal userscript when a browser lacks the managed user-script channel.
  try { await api.configureWorld({worldId: registrationWorldID(value.metadata.id), messaging: true}); } catch { /* unavailable */ }
}

function nativeAPI(): NativeUserScripts | undefined {
  return (browser as unknown as {userScripts?: NativeUserScripts}).userScripts;
}

async function readRecords(): Promise<Record<string, UserScriptManifest>> {
  const stored = await browser.storage.local.get(storageKey);
  const value = stored[storageKey];
  return isRecord(value) ? value as unknown as Record<string, UserScriptManifest> : {};
}

async function writeRecords(value: Record<string, UserScriptManifest>) {
  await browser.storage.local.set({[storageKey]: value});
  await syncNativeCatalog(value);
}

async function syncNativeCatalog(value: Record<string, UserScriptManifest>) {
  if (import.meta.env.BROWSER !== 'safari') return;
  const runtime = browser.runtime as typeof browser.runtime & {
    sendNativeMessage?: (application: string, message: unknown) => Promise<unknown>;
  };
  if (!runtime.sendNativeMessage) return;
  const values = Object.values(value).map((manifest) => ({
    id: manifest.metadata.id,
    name: manifest.metadata.name,
    revision: manifest.metadata.revision,
    enabled: manifest.spec.enabled,
    matches: [...manifest.spec.matches],
  }));
  try {
    await runtime.sendNativeMessage('dev.jangolova.Jangolova', {
      method: 'userscripts.catalog.replace',
      values,
    });
  } catch {
    // The containing app is optional. Userscript state remains authoritative here.
  }
}

function requireID(value: unknown) {
  if (typeof value !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value)) throw new Error('valid userscript id is required');
  return value;
}

function requireText(value: unknown, name: string, maximum: number) {
  if (typeof value !== 'string' || !value.trim() || value.length > maximum) throw new Error(`${name} must be a bounded non-empty string`);
  return value;
}

function requireStrings(value: unknown, name: string) {
  if (!Array.isArray(value) || value.length === 0 || value.length > 128 || value.some((item) => typeof item !== 'string' || !item)) {
    throw new Error(`${name} must be a bounded non-empty string array`);
  }
  return [...value] as string[];
}

function requireBoolean(value: unknown, name: string) {
  if (typeof value !== 'boolean') throw new Error(`${name} must be boolean`);
  return value;
}

function requireRunAt(value: unknown): 'document_start' | 'document_end' | 'document_idle' {
  if (value === 'document_start' || value === 'document_end' || value === 'document_idle') return value;
  throw new Error('userscript runAt is invalid');
}

function requireWorld(value: unknown): 'USER_SCRIPT' | 'MAIN' {
  if (value === 'USER_SCRIPT' || value === 'MAIN') return value;
  throw new Error('userscript world is invalid');
}

function eventData(value: UserScriptManifest) {
  return {id: value.metadata.id, revision: value.metadata.revision, enabled: value.spec.enabled};
}
