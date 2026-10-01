import type {createApprovalManager} from './approvals.js';
import type {createBrowserAdapter} from './runtime.js';
import type {createPackageCatalog} from './catalog.js';
import type {createTabRouter} from './tabs.js';

export type AugmentationMount = {
  augmentationId: string;
  package: string;
  target?: {tabId?: number};
  permissions?: string[];
  configuration?: Record<string, unknown>;
  approvalId?: string;
};

export type AugmentationDependencies = {
  catalog: ReturnType<typeof createPackageCatalog>;
  approvals: ReturnType<typeof createApprovalManager>;
  tabs: ReturnType<typeof createTabRouter>;
  browser: ReturnType<typeof createBrowserAdapter>;
  packagePath?: (packageId: string, entrypoint: string) => string;
};

const identifier = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export function createAugmentationManager(dependencies: AugmentationDependencies) {
  if (!dependencies?.catalog?.require || !dependencies.approvals?.request || !dependencies.tabs?.resolve
    || !dependencies.tabs.send || !dependencies.browser?.scripts?.execute) throw new Error('augmentation dependencies are incomplete');
  const path = dependencies.packagePath ?? ((id: string, entrypoint: string) => `augmentations/${id}/${entrypoint}`);
  return {
    async mount(input: AugmentationMount) {
      if (!input || typeof input.augmentationId !== 'string' || !identifier.test(input.augmentationId)) throw new Error('augmentation id is invalid');
      const item = await dependencies.catalog.require(input.package, input.permissions ?? []);
      const tab = await dependencies.tabs.resolve(input.target);
      const origin = safeOrigin(tab.url);
      const approval = await dependencies.approvals.request({
        kind: 'package', subjectId: item.description.id, augmentationId: input.augmentationId, revision: item.description.version,
        targetId: String(tab.id), origin, permissions: item.permissions,
      }, input.approvalId);
      if (!approval.approved) return {status: 'approval-required' as const, approval: approval.approval};
      const payload = {...input, package: item.description.id, augmentationId: input.augmentationId, target: {tabId: tab.id}};
      if (item.delivery.kind === 'sandbox') {
        return dependencies.tabs.send({tabId: tab.id}, 'cymonkey.jangolova.sandbox', 'mount', payload);
      }
      await dependencies.browser.scripts.execute(item.description.id, tab.id, [path(item.description.id, item.delivery.entrypoint)]);
      return dependencies.tabs.send({tabId: tab.id}, 'cymonkey.jangolova.augmentation-runtime', 'mount', payload);
    },
    async unmount(input: Pick<AugmentationMount, 'augmentationId' | 'package' | 'target'>) {
      if (!input || typeof input.augmentationId !== 'string' || !identifier.test(input.augmentationId)) throw new Error('augmentation id is invalid');
      const item = await dependencies.catalog.require(input.package, []);
      const tab = await dependencies.tabs.resolve(input.target);
      const channel = item.delivery.kind === 'sandbox' ? 'cymonkey.jangolova.sandbox' : 'cymonkey.jangolova.augmentation-runtime';
      return dependencies.tabs.send({tabId: tab.id}, channel, 'unmount', {...input, target: {tabId: tab.id}});
    },
  };
}

function safeOrigin(value?: string) {
  if (!value) return 'unknown';
  try { return new URL(value).origin; } catch { return 'unknown'; }
}
