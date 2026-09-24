export type ExtensionDescription = {
  id: string;
  name: string;
  version: string;
  enabled: boolean;
  installType: string;
  permissions: string[];
  hostPermissions: string[];
  mayDisable: boolean;
  mayEnable?: boolean;
};

export type NativeExtensionInfo = ExtensionDescription & {type: string};

export type ManagementEvent = {type: 'installed' | 'enabled' | 'disabled'; extension: ExtensionDescription}
  | {type: 'uninstalled'; id: string};

export type NativeEvent<Arg> = {
  addListener(listener: (value: Arg) => void): void;
  removeListener(listener: (value: Arg) => void): void;
};

// Structural types let Chrome, Firefox, or a test adapter supply their own API.
export type NativeManagement = {
  getAll(): Promise<NativeExtensionInfo[]>;
  get(id: string): Promise<NativeExtensionInfo>;
  setEnabled?(id: string, enabled: boolean): Promise<void>;
  uninstall?(id: string): Promise<void>;
  onInstalled?: NativeEvent<NativeExtensionInfo>;
  onEnabled?: NativeEvent<NativeExtensionInfo>;
  onDisabled?: NativeEvent<NativeExtensionInfo>;
  onUninstalled?: NativeEvent<string>;
};

export type InstallRequest = {
  source: string;
  revision?: string;
  target?: Record<string, unknown>;
};

// Implement this in a trusted host using Cymonkey/Jangolova's packaging and
// browser-specific install actions. It is never a raw browser API proxy.
export type InstallBackend<Result = unknown> = {
  prepare(source: string): Promise<{revision: string; [key: string]: unknown}>;
  install(request: InstallRequest): Promise<Result>;
};

export type ManagerCapabilities = {
  list: boolean;
  describe: boolean;
  setEnabled: boolean;
  uninstall: boolean;
  prepare: boolean;
  install: boolean;
  events: boolean;
};

const idPattern = /^[A-Za-z0-9@._-]{1,128}$/;

export function createExtensionManager<Result = unknown>(native: NativeManagement, installBackend?: InstallBackend<Result>) {
  if (!native || typeof native.getAll !== 'function' || typeof native.get !== 'function') {
    throw new Error('a browser management adapter with getAll and get is required');
  }
  const describe = async (id: string): Promise<ExtensionDescription> => {
    const item = await native.get(requireID(id));
    if (item.type !== 'extension') throw new Error('browser item is not an extension');
    return description(item);
  };
  return {
    capabilities(): ManagerCapabilities {
      return {
        list: true, describe: true,
        setEnabled: typeof native.setEnabled === 'function',
        uninstall: typeof native.uninstall === 'function',
        prepare: typeof installBackend?.prepare === 'function',
        install: typeof installBackend?.install === 'function',
        events: Boolean(native.onInstalled || native.onEnabled || native.onDisabled || native.onUninstalled),
      };
    },
    async list(): Promise<ExtensionDescription[]> {
      return (await native.getAll()).filter((item) => item.type === 'extension')
        .map(description).sort((a, b) => a.id.localeCompare(b.id));
    },
    describe,
    async setEnabled(id: string, enabled: boolean): Promise<ExtensionDescription> {
      if (typeof enabled !== 'boolean') throw new Error('enabled must be a boolean');
      if (!native.setEnabled) throw new Error('enable/disable is unavailable in this browser adapter');
      const current = await describe(id);
      if (current.enabled === enabled) return current;
      if (!enabled && !current.mayDisable) throw new Error('this extension cannot be disabled');
      if (enabled && current.mayEnable === false) throw new Error('this extension cannot be enabled');
      await native.setEnabled(current.id, enabled);
      return describe(current.id);
    },
    async uninstall(id: string): Promise<{uninstalled: string}> {
      if (!native.uninstall) throw new Error('uninstall is unavailable in this browser adapter');
      const current = await describe(id);
      if (!current.mayDisable) throw new Error('this extension cannot be uninstalled');
      await native.uninstall(current.id);
      return {uninstalled: current.id};
    },
    async prepare(source: string) {
      if (!installBackend?.prepare) throw new Error('installation host is unavailable');
      if (typeof source !== 'string' || !source) throw new Error('source is required');
      return installBackend.prepare(source);
    },
    async install(request: InstallRequest): Promise<Result> {
      if (!installBackend?.install) throw new Error('installation host is unavailable');
      if (!request || typeof request.source !== 'string' || !request.source) throw new Error('source is required');
      return installBackend.install(request);
    },
    watch(listener: (event: ManagementEvent) => void): () => void {
      const bindings: Array<() => void> = [];
      for (const [type, source] of [
        ['installed', native.onInstalled], ['enabled', native.onEnabled], ['disabled', native.onDisabled],
      ] as const) {
        if (!source) continue;
        const handler = (item: NativeExtensionInfo) => {
          if (item.type === 'extension') listener({type, extension: description(item)});
        };
        source.addListener(handler);
        bindings.push(() => source.removeListener(handler));
      }
      if (native.onUninstalled) {
        const handler = (id: string) => listener({type: 'uninstalled', id});
        native.onUninstalled.addListener(handler);
        bindings.push(() => native.onUninstalled?.removeListener(handler));
      }
      return () => { for (const unbind of bindings) unbind(); };
    },
  };
}

function requireID(value: string) {
  if (typeof value !== 'string' || !idPattern.test(value)) throw new Error('valid browser extension ID is required');
  return value;
}

function description(item: NativeExtensionInfo): ExtensionDescription {
  return {
    id: item.id, name: item.name, version: item.version,
    enabled: item.enabled, installType: item.installType,
    permissions: [...item.permissions].sort(),
    hostPermissions: [...item.hostPermissions].sort(),
    mayDisable: item.mayDisable,
    ...(item.mayEnable === undefined ? {} : {mayEnable: item.mayEnable}),
  };
}
