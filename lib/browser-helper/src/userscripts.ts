import type {StorageArea} from './runtime.js';

export type ManagedUserscript = {metadata: {id: string; revision: string}; spec: {enabled: boolean}};
export type NativeUserscriptRegistration = {id: string; [key: string]: unknown};
export type NativeUserscriptAPI<Registration extends NativeUserscriptRegistration> = {
  getScripts(): Promise<Array<{id: string}>>;
  register(scripts: Registration[]): Promise<void>;
  update(scripts: Registration[]): Promise<void>;
  unregister(filter: {ids: string[]}): Promise<void>;
};
export type UserscriptManagerDependencies<Manifest extends ManagedUserscript, Registration extends NativeUserscriptRegistration, Description> = {
  storage: StorageArea;
  native: NativeUserscriptAPI<Registration>;
  validate: (value: unknown) => Promise<Manifest> | Manifest;
  describe: (manifest: Manifest) => Description;
  registration: (manifest: Manifest) => Registration;
  registrationId: (scriptId: string) => string;
  ownsRegistrationId: (registrationId: string) => boolean;
  authorize: (operation: 'install' | 'update', next: Manifest, previous?: Manifest) => Promise<void>;
  onChange?: (type: 'installed' | 'updated' | 'uninstalled' | 'enabled' | 'disabled', description: Description) => Promise<void> | void;
  storageKey?: string;
};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export function createNativeUserscriptManager<Manifest extends ManagedUserscript, Registration extends NativeUserscriptRegistration, Description>(
  dependencies: UserscriptManagerDependencies<Manifest, Registration, Description>,
) {
  if (!dependencies?.storage?.get || !dependencies.storage.set || !dependencies.native?.getScripts
    || !dependencies.native.register || !dependencies.native.update || !dependencies.native.unregister
    || !dependencies.validate || !dependencies.describe || !dependencies.registration
    || !dependencies.registrationId || !dependencies.ownsRegistrationId || !dependencies.authorize) {
    throw new Error('userscript manager dependencies are incomplete');
  }
  const key = dependencies.storageKey ?? 'cymonkey.browserAdapter.userscripts.v1';
  let queue = Promise.resolve<unknown>(undefined);
  const serialize = <T>(operation: () => Promise<T>): Promise<T> => {
    const result = queue.then(operation, operation);
    queue = result.catch(() => undefined);
    return result;
  };
  async function read(): Promise<Record<string, Manifest>> {
    const stored = await dependencies.storage.get(key);
    const value = stored[key];
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    const records = Object.create(null) as Record<string, Manifest>;
    for (const [id, raw] of Object.entries(value)) {
      const {manifest} = await validated(raw);
      if (id !== manifest.metadata.id) throw new Error('stored userscript id does not match its manifest');
      records[id] = manifest;
    }
    return records;
  }
  async function write(records: Record<string, Manifest>) { await dependencies.storage.set({[key]: records}); }
  async function changed(type: 'installed' | 'updated' | 'uninstalled' | 'enabled' | 'disabled', manifest: Manifest) {
    await dependencies.onChange?.(type, dependencies.describe(manifest));
  }
  async function validated(value: unknown) {
    const manifest = await dependencies.validate(value);
    if (!manifest || typeof manifest.metadata?.id !== 'string' || !identifier.test(manifest.metadata.id)
      || typeof manifest.metadata.revision !== 'string' || !manifest.metadata.revision
      || typeof manifest.spec?.enabled !== 'boolean') throw new Error('userscript manifest is invalid');
    const registration = dependencies.registration(manifest);
    if (!registration || registration.id !== dependencies.registrationId(manifest.metadata.id)
      || !dependencies.ownsRegistrationId(registration.id)) throw new Error('userscript registration is invalid');
    return {manifest, registration};
  }
  async function requireNative() {
    try { await dependencies.native.getScripts(); }
    catch { throw new Error('browser userscript permission or user setting is disabled'); }
  }
  return {
    async availability() {
      try { await requireNative(); return {available: true}; }
      catch { return {available: false}; }
    },
    list: () => serialize(async () => Object.values(await read()).sort((a, b) => a.metadata.id.localeCompare(b.metadata.id)).map(dependencies.describe)),
    describe: (id: string) => serialize(async () => {
      if (typeof id !== 'string' || !identifier.test(id)) throw new Error('userscript id is invalid');
      const item = (await read())[id];
      if (!item) throw new Error('userscript is not installed');
      return dependencies.describe(item);
    }),
    install: (value: unknown) => serialize(async () => {
      const {manifest, registration} = await validated(value);
      const records = await read();
      if (records[manifest.metadata.id]) throw new Error('userscript is already installed');
      await dependencies.authorize('install', manifest);
      if (manifest.spec.enabled) { await requireNative(); await dependencies.native.register([registration]); }
      records[manifest.metadata.id] = manifest;
      await write(records);
      await changed('installed', manifest);
      return dependencies.describe(manifest);
    }),
    update: (value: unknown) => serialize(async () => {
      const {manifest, registration} = await validated(value);
      const records = await read();
      const previous = records[manifest.metadata.id];
      if (!previous) throw new Error('userscript is not installed');
      await dependencies.authorize('update', manifest, previous);
      if (previous.spec.enabled || manifest.spec.enabled) await requireNative();
      if (previous.spec.enabled && manifest.spec.enabled) await dependencies.native.update([registration]);
      else if (previous.spec.enabled) await dependencies.native.unregister({ids: [registration.id]});
      else if (manifest.spec.enabled) await dependencies.native.register([registration]);
      records[manifest.metadata.id] = manifest;
      await write(records);
      await changed('updated', manifest);
      return dependencies.describe(manifest);
    }),
    setEnabled: (id: string, enabled: boolean) => serialize(async () => {
      if (typeof id !== 'string' || !identifier.test(id) || typeof enabled !== 'boolean') throw new Error('userscript enable request is invalid');
      const records = await read();
      const previous = records[id];
      if (!previous) throw new Error('userscript is not installed');
      if (previous.spec.enabled === enabled) return dependencies.describe(previous);
      const next = structuredClone(previous);
      next.spec.enabled = enabled;
      const {registration} = await validated(next);
      await requireNative();
      if (enabled) await dependencies.native.register([registration]);
      else await dependencies.native.unregister({ids: [registration.id]});
      records[id] = next;
      await write(records);
      await changed(enabled ? 'enabled' : 'disabled', next);
      return dependencies.describe(next);
    }),
    uninstall: (id: string) => serialize(async () => {
      if (typeof id !== 'string' || !identifier.test(id)) throw new Error('userscript id is invalid');
      const records = await read();
      const previous = records[id];
      if (!previous) throw new Error('userscript is not installed');
      if (previous.spec.enabled) { await requireNative(); await dependencies.native.unregister({ids: [dependencies.registrationId(id)]}); }
      delete records[id];
      await write(records);
      await changed('uninstalled', previous);
      return {id};
    }),
    reconcile: () => serialize(async () => {
      await requireNative();
      const records = await read();
      const expected = Object.values(records).filter((manifest) => manifest.spec.enabled);
      const ids = new Set(expected.map((manifest) => dependencies.registrationId(manifest.metadata.id)));
      const actual = await dependencies.native.getScripts();
      const registered = new Set(actual.map((item) => item.id));
      const orphaned = actual.map((item) => item.id).filter((id) => dependencies.ownsRegistrationId(id) && !ids.has(id));
      const missing = expected.filter((manifest) => !registered.has(dependencies.registrationId(manifest.metadata.id)));
      if (orphaned.length) await dependencies.native.unregister({ids: orphaned});
      if (missing.length) await dependencies.native.register(missing.map(dependencies.registration));
      return {registered: expected.length, restored: missing.length, removed: orphaned.length};
    }),
  };
}
