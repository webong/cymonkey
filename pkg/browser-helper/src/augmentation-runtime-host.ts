export type AugmentationRuntime = {
  packageId: string;
  dispatch(request: unknown): unknown | Promise<unknown>;
  unmount(): void | Promise<void>;
};
export type AugmentationFactory = (context: {packageId: string; augmentationId: string; configuration: Record<string, unknown>}) => AugmentationRuntime | Promise<AugmentationRuntime>;
export type AugmentationRuntimeEvent = {type: 'augmentation.mounted' | 'augmentation.unmounted'; packageId: string; augmentationId: string};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export function createAugmentationRuntimeHost(dependencies: {
  factories: Map<string, AugmentationFactory>;
  onEvent?: (event: AugmentationRuntimeEvent) => void | Promise<void>;
}) {
  if (!(dependencies?.factories instanceof Map)) throw new Error('augmentation factory registry is required');
  const active = new Map<string, AugmentationRuntime>();
  // An action must finish before cleanup starts. Independent augmentations
  // remain concurrent; failures must not poison later cleanup or retry.
  const queues = new Map<string, Promise<unknown>>();
  const serialize = <T>(id: string, operation: () => Promise<T>): Promise<T> => {
    const result = (queues.get(id) ?? Promise.resolve()).then(operation, operation);
    const settled = result.then(() => undefined, () => undefined);
    queues.set(id, settled);
    void settled.then(() => { if (queues.get(id) === settled) queues.delete(id); });
    return result;
  };
  const emit = async (event: AugmentationRuntimeEvent) => {
    try { await dependencies.onEvent?.(event); } catch { /* runtime state remains authoritative */ }
  };
  return {
    list: () => [...active.keys()].sort(),
    async mount(input: {packageId: string; augmentationId: string; configuration?: Record<string, unknown>}) {
      if (!input || typeof input.packageId !== 'string' || !identifier.test(input.packageId)
        || typeof input.augmentationId !== 'string' || !identifier.test(input.augmentationId)) throw new Error('augmentation mount ids are invalid');
      const {packageId, augmentationId} = input;
      return serialize(augmentationId, async () => {
        if (active.has(augmentationId)) throw new Error('augmentation already exists');
        const factory = dependencies.factories.get(packageId);
        if (!factory) throw new Error('augmentation package did not register');
        const configuration = input.configuration && typeof input.configuration === 'object' && !Array.isArray(input.configuration) ? input.configuration : {};
        const runtime = await factory({packageId, augmentationId, configuration});
        if (!runtime || runtime.packageId !== packageId || typeof runtime.dispatch !== 'function' || typeof runtime.unmount !== 'function') {
          if (typeof runtime?.unmount === 'function') await runtime.unmount();
          throw new Error('augmentation package returned an invalid runtime');
        }
        active.set(augmentationId, runtime);
        await emit({type: 'augmentation.mounted', packageId, augmentationId});
        return {packageId, augmentationId};
      });
    },
    async call(augmentationId: string, request: unknown) {
      return serialize(augmentationId, async () => {
        const runtime = active.get(augmentationId);
        if (!runtime) throw new Error('augmentation does not exist');
        return runtime.dispatch(request);
      });
    },
    async unmount(augmentationId: string, packageId?: string) {
      return serialize(augmentationId, async () => {
        const runtime = active.get(augmentationId);
        if (!runtime) throw new Error('augmentation does not exist');
        if (packageId !== undefined && packageId !== runtime.packageId) throw new Error('augmentation belongs to a different package');
        await runtime.unmount();
        active.delete(augmentationId);
        await emit({type: 'augmentation.unmounted', packageId: runtime.packageId, augmentationId});
        return {packageId: runtime.packageId, augmentationId};
      });
    },
  };
}
