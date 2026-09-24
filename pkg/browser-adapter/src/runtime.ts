export type StorageArea = {
  get(keys: string | string[]): Promise<Record<string, unknown>>;
  set(values: Record<string, unknown>): Promise<void>;
};

export type ScriptRegistration = {
  id: string;
  matches: string[];
  files: string[];
  excludeMatches?: string[];
  runAt?: 'document_start' | 'document_end' | 'document_idle';
  allFrames?: boolean;
  persistAcrossSessions?: boolean;
  world?: 'ISOLATED' | 'MAIN';
};

export type NetworkRule = {id: number; [key: string]: unknown};

export type BrowserAdapterEvent = {
  id: string;
  type: string;
  occurredAt: string;
  owner: string;
  data: Record<string, unknown>;
};

export type BrowserAdapterDependencies = {
  storage: StorageArea;
  scripting?: {
    registerContentScripts(scripts: Array<Record<string, unknown>>): Promise<void>;
    unregisterContentScripts(options: {ids: string[]}): Promise<void>;
    executeScript?(options: {target: {tabId: number; allFrames?: boolean}; files: string[]; world: 'ISOLATED' | 'MAIN'}): Promise<Array<{frameId: number}>>;
    insertCSS?(options: {target: {tabId: number; allFrames?: boolean}; css: string}): Promise<void>;
    removeCSS?(options: {target: {tabId: number; allFrames?: boolean}; css: string}): Promise<void>;
  };
  network?: {
    updateDynamicRules(options: {removeRuleIds: number[]; addRules?: NetworkRule[]}): Promise<void>;
  };
  assetPrefix?: (owner: string) => string;
  now?: () => string;
};

const ownerPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const keyPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const ownershipKey = 'cymonkey.browserAdapter.networkOwnership.v1';
const eventsKey = 'cymonkey.browserAdapter.events.v1';
const sequenceKey = 'cymonkey.browserAdapter.eventSequence.v1';

export function createBrowserAdapter(dependencies: BrowserAdapterDependencies) {
  if (!dependencies?.storage?.get || !dependencies.storage.set) throw new Error('browser storage adapter is required');
  const {storage, scripting, network} = dependencies;
  const assetPrefix = dependencies.assetPrefix ?? ((owner: string) => `augmentations/${owner}/`);
  let queue = Promise.resolve<unknown>(undefined);
  const serialize = <T>(operation: () => Promise<T>): Promise<T> => {
    const result = queue.then(operation, operation);
    queue = result.catch(() => undefined);
    return result;
  };

  async function appendEvent(type: string, owner: string, data: Record<string, unknown> = {}): Promise<BrowserAdapterEvent> {
    checkOwner(owner);
    if (!/^[a-z][a-z0-9._-]{0,127}$/.test(type)) throw new Error('event type is invalid');
    return serialize(async () => {
      const stored = await storage.get([eventsKey, sequenceKey]);
      const sequence = Number(stored[sequenceKey] || 0) + 1;
      const events = Array.isArray(stored[eventsKey]) ? [...stored[eventsKey]] as BrowserAdapterEvent[] : [];
      const event = {id: String(sequence), type, owner, occurredAt: dependencies.now?.() ?? new Date().toISOString(), data: {...data}};
      events.push(event);
      if (events.length > 256) events.splice(0, events.length - 256);
      await storage.set({[eventsKey]: events, [sequenceKey]: sequence});
      return event;
    });
  }

  async function ownership(): Promise<Record<string, string>> {
    const stored = await storage.get(ownershipKey);
    const value = stored[ownershipKey];
    if (!record(value)) return {};
    return Object.fromEntries(Object.entries(value).filter((entry): entry is [string, string] => typeof entry[1] === 'string'));
  }

  return {
    capabilities() {
      return {storage: true, events: true, scripts: Boolean(scripting), networkRules: Boolean(network)};
    },
    storage: {
      async get(owner: string, keys: string[]) {
        checkOwner(owner);
        const validated = checkKeys(keys);
        const prefix = `cymonkey.scoped.${owner}.`;
        const stored = await storage.get(validated.map((key) => prefix + key));
        return Object.fromEntries(validated.map((key) => [key, stored[prefix + key] ?? null]));
      },
      async set(owner: string, values: Record<string, unknown>) {
        checkOwner(owner);
        if (!record(values)) throw new Error('storage values must be an object');
        const keys = checkKeys(Object.keys(values));
        await storage.set(Object.fromEntries(keys.map((key) => [`cymonkey.scoped.${owner}.${key}`, values[key]])));
        await appendEvent('storage.updated', owner, {keys: [...keys].sort()});
        return {keys: [...keys].sort()};
      },
    },
    events: {
      append: appendEvent,
      async read(options: {after?: string; limit?: number; types?: string[]} = {}) {
        const after = Number(options.after ?? '0');
        if (!Number.isSafeInteger(after) || after < 0) throw new Error('events.after must be a non-negative integer');
        const limit = Math.min(Math.max(options.limit ?? 100, 1), 256);
        const types = new Set(options.types ?? []);
        const stored = await storage.get([eventsKey, sequenceKey]);
        const events = Array.isArray(stored[eventsKey]) ? stored[eventsKey] as BrowserAdapterEvent[] : [];
        return {events: events.filter((event) => Number(event.id) > after && (!types.size || types.has(event.type))).slice(0, limit), cursor: String(stored[sequenceKey] ?? 0)};
      },
    },
    scripts: {
      async register(owner: string, requested: ScriptRegistration[]) {
        checkOwner(owner);
        if (!scripting) throw new Error('browser scripting adapter is unavailable');
        if (!Array.isArray(requested) || requested.length === 0) throw new Error('scripts must be a non-empty array');
        const scripts = requested.map((script) => normalizeScript(owner, script, assetPrefix));
        await scripting.registerContentScripts(scripts);
        await appendEvent('script.registered', owner, {ids: scripts.map((script) => script.id)});
        return {ids: scripts.map((script) => script.id)};
      },
      async unregister(owner: string, ids: string[]) {
        checkOwner(owner);
        if (!scripting) throw new Error('browser scripting adapter is unavailable');
        if (!Array.isArray(ids) || ids.length === 0) throw new Error('ids must be a non-empty array');
        const scoped = ids.map((id) => scriptID(owner, id));
        await scripting.unregisterContentScripts({ids: scoped});
        await appendEvent('script.unregistered', owner, {ids: scoped});
        return {ids: scoped};
      },
      async execute(owner: string, tabId: number, files: string[], options: {allFrames?: boolean; world?: 'ISOLATED' | 'MAIN'} = {}) {
        checkOwner(owner);
        if (!scripting?.executeScript) throw new Error('browser script execution is unavailable');
        checkTab(tabId);
        const checked = checkFiles(owner, files, assetPrefix);
        const results = await scripting.executeScript({target: {tabId, allFrames: options.allFrames === true}, files: checked, world: options.world ?? 'ISOLATED'});
        await appendEvent('script.executed', owner, {tabId, frames: results.map((result) => result.frameId)});
        return {tabId, frames: results.map((result) => result.frameId)};
      },
      async style(owner: string, tabId: number, css: string, operation: 'insert' | 'remove') {
        checkOwner(owner);
        checkTab(tabId);
        if (typeof css !== 'string' || !css) throw new Error('css is required');
        if (operation === 'insert' && scripting?.insertCSS) await scripting.insertCSS({target: {tabId}, css});
        else if (operation === 'remove' && scripting?.removeCSS) await scripting.removeCSS({target: {tabId}, css});
        else throw new Error('browser style action is unavailable');
        await appendEvent(`style.${operation === 'insert' ? 'inserted' : 'removed'}`, owner, {tabId});
        return {tabId};
      },
    },
    network: {
      async install(owner: string, rules: NetworkRule[]) {
        checkOwner(owner);
        if (!network) throw new Error('browser network-rule adapter is unavailable');
        const checked = checkRules(rules);
        return serialize(async () => {
          const owners = await ownership();
          for (const rule of checked) if (owners[String(rule.id)] && owners[String(rule.id)] !== owner) throw new Error(`network rule ${rule.id} belongs to another owner`);
          const ids = checked.map((rule) => rule.id);
          await network.updateDynamicRules({removeRuleIds: ids, addRules: checked});
          for (const id of ids) owners[String(id)] = owner;
          await storage.set({[ownershipKey]: owners});
          return {ids};
        }).then(async (result) => { await appendEvent('network.rules.installed', owner, result); return result; });
      },
      async remove(owner: string, ids: number[]) {
        checkOwner(owner);
        if (!network) throw new Error('browser network-rule adapter is unavailable');
        checkRuleIDs(ids);
        return serialize(async () => {
          const owners = await ownership();
          for (const id of ids) if (owners[String(id)] !== owner) throw new Error(`network rule ${id} is not owned by ${owner}`);
          await network.updateDynamicRules({removeRuleIds: ids});
          for (const id of ids) delete owners[String(id)];
          await storage.set({[ownershipKey]: owners});
          return {ids};
        }).then(async (result) => { await appendEvent('network.rules.removed', owner, result); return result; });
      },
    },
  };
}

function checkOwner(owner: string) {
  if (!ownerPattern.test(owner)) throw new Error('valid owner id is required');
}
function checkKeys(keys: string[]) {
  if (!Array.isArray(keys) || keys.length > 256 || keys.some((key) => typeof key !== 'string' || !keyPattern.test(key))) throw new Error('storage keys must be bounded identifiers');
  return keys;
}
function checkTab(tabId: number) {
  if (!Number.isInteger(tabId) || tabId < 0) throw new Error('valid tab id is required');
}
function scriptID(owner: string, id: string) {
  if (!keyPattern.test(id)) throw new Error('valid script id is required');
  const encodedOwner = encodeScriptPart(owner);
  const scoped = `cm-${encodedOwner.length}-${encodedOwner}-${encodeScriptPart(id)}`;
  if (scoped.length > 128) throw new Error('registered script id is too long');
  return scoped;
}
function encodeScriptPart(value: string) {
  return value.replaceAll('_', '_5f').replaceAll('.', '_2e');
}
function checkFiles(owner: string, files: string[], prefixFor: (owner: string) => string) {
  if (!Array.isArray(files) || !files.length) throw new Error('files must be a non-empty array');
  const prefix = prefixFor(owner);
  if (!prefix || prefix.startsWith('/') || prefix.includes('..') || !prefix.endsWith('/')) throw new Error('asset prefix is invalid');
  return files.map((file) => {
    if (typeof file !== 'string' || !file.startsWith(prefix) || file.includes('..') || /[:?#\\]/.test(file)) throw new Error(`file must be packaged below ${prefix}`);
    return file;
  });
}
function normalizeScript(owner: string, script: ScriptRegistration, prefixFor: (owner: string) => string) {
  if (!script || !Array.isArray(script.matches) || !script.matches.length || script.matches.some((match) => typeof match !== 'string')) throw new Error('script matches are required');
  return {
    id: scriptID(owner, script.id), matches: [...script.matches], js: checkFiles(owner, script.files, prefixFor),
    excludeMatches: script.excludeMatches ?? [], runAt: script.runAt ?? 'document_idle',
    allFrames: script.allFrames === true, persistAcrossSessions: script.persistAcrossSessions !== false,
    world: script.world ?? 'ISOLATED',
  };
}
function checkRuleIDs(ids: number[]) {
  if (!Array.isArray(ids) || !ids.length || ids.some((id) => !Number.isInteger(id) || id <= 0) || new Set(ids).size !== ids.length) throw new Error('rule ids must be unique positive integers');
}
function checkRules(rules: NetworkRule[]) {
  if (!Array.isArray(rules) || !rules.length || rules.some((rule) => !record(rule) || !Number.isInteger(rule.id) || rule.id <= 0)) throw new Error('rules must have positive integer ids');
  checkRuleIDs(rules.map((rule) => rule.id));
  return rules;
}
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
