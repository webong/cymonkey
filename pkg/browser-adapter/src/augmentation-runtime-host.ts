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
  const pending = new Set<string>();
  const emit = async (event: AugmentationRuntimeEvent) => {
    try { await dependencies.onEvent?.(event); } catch { /* runtime state remains authoritative */ }
  };
  return {
    list: () => [...active.keys()].sort(),
    async mount(input: {packageId: string; augmentationId: string; configuration?: Record<string, unknown>}) {
      if (!input || typeof input.packageId !== 'string' || !identifier.test(input.packageId)
        || typeof input.augmentationId !== 'string' || !identifier.test(input.augmentationId)) throw new Error('augmentation mount ids are invalid');
      const {packageId, augmentationId} = input;
      if (active.has(augmentationId) || pending.has(augmentationId)) throw new Error('augmentation already exists');
      const factory = dependencies.factories.get(packageId);
      if (!factory) throw new Error('augmentation package did not register');
      pending.add(augmentationId);
      try {
        const configuration = input.configuration && typeof input.configuration === 'object' && !Array.isArray(input.configuration) ? input.configuration : {};
        const runtime = await factory({packageId, augmentationId, configuration});
        if (!runtime || runtime.packageId !== packageId || typeof runtime.dispatch !== 'function' || typeof runtime.unmount !== 'function') {
          throw new Error('augmentation package returned an invalid runtime');
        }
        active.set(augmentationId, runtime);
        await emit({type: 'augmentation.mounted', packageId, augmentationId});
        return {packageId, augmentationId};
      } finally { pending.delete(augmentationId); }
    },
    async call(augmentationId: string, request: unknown) {
      const runtime = active.get(augmentationId);
      if (!runtime) throw new Error('augmentation does not exist');
      return runtime.dispatch(request);
    },
    async unmount(augmentationId: string, packageId?: string) {
      const runtime = active.get(augmentationId);
      if (!runtime) throw new Error('augmentation does not exist');
      if (packageId !== undefined && packageId !== runtime.packageId) throw new Error('augmentation belongs to a different package');
      await runtime.unmount();
      active.delete(augmentationId);
      await emit({type: 'augmentation.unmounted', packageId: runtime.packageId, augmentationId});
      return {packageId: runtime.packageId, augmentationId};
    },
  };
}
