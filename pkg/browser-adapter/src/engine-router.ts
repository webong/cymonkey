import type {createTabRouter} from './tabs.js';

export type EngineDelivery = 'page-runtime' | 'augmentation-package' | 'sandbox';
export type EngineRouterDependencies = {
  tabs: ReturnType<typeof createTabRouter>;
  executeMainWorld: (tabId: number, request: unknown) => Promise<unknown>;
  onComplete?: (event: {tabId: number; augmentationId?: string; sandboxId?: string; delivery: EngineDelivery}) => void | Promise<void>;
};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const valid = (value: unknown): value is string => typeof value === 'string' && identifier.test(value);

export function createEngineRouter(dependencies: EngineRouterDependencies) {
  if (!dependencies?.tabs?.resolve || !dependencies.tabs.send || typeof dependencies.executeMainWorld !== 'function') {
    throw new Error('engine router dependencies are incomplete');
  }
  return {
    async call(input: {request: unknown; target?: {tabId?: number}; augmentationId?: string; delivery?: EngineDelivery; sandboxId?: string}) {
      const delivery = input?.delivery ?? 'page-runtime';
      if (!['page-runtime', 'augmentation-package', 'sandbox'].includes(delivery)) throw new Error('unsupported engine delivery');
      const tab = await dependencies.tabs.resolve(input.target);
      let result: unknown;
      if (delivery === 'sandbox') {
        if (!valid(input.augmentationId) || !valid(input.sandboxId)) throw new Error('sandbox delivery requires valid augmentation and sandbox IDs');
        result = await dependencies.tabs.send({tabId: tab.id}, 'cymonkey.jangolova.sandbox', 'call', {
          augmentationId: input.augmentationId, id: input.sandboxId, request: input.request,
        });
      } else if (delivery === 'augmentation-package') {
        if (!valid(input.augmentationId)) throw new Error('package delivery requires a valid augmentation ID');
        result = await dependencies.tabs.send({tabId: tab.id}, 'cymonkey.jangolova.augmentation-runtime', 'dispatch', {
          augmentationId: input.augmentationId, request: input.request,
        });
      } else {
        result = await dependencies.executeMainWorld(tab.id, input.request);
      }
      await dependencies.onComplete?.({tabId: tab.id, augmentationId: input.augmentationId, sandboxId: input.sandboxId, delivery});
      return result;
    },
  };
}
