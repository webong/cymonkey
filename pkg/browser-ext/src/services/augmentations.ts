import {authorizePackageMount} from './approvals';
import {requireBrowserPackage} from './packages';
import {requireScopedIdentifier} from './policy';
import {requireTabID, sendToTab, sendToTabChannel, targetTab} from './tabs';

export async function mountReviewedAugmentation(input: Record<string, unknown>) {
  const packageValue = await requireBrowserPackage(input.package, input.permissions);
  const augmentationId = requireScopedIdentifier(input.augmentationId, 'augmentationId');
  const tab = await targetTab(input.target);
  const tabId = requireTabID(tab);
  const approval = await authorizePackageMount({
    approvalId: input.approvalId,
    packageId: packageValue.description.id,
    packageName: packageValue.description.name,
    permissions: packageValue.permissions,
    augmentationId,
    tabId,
    origin: safeOrigin(tab.url),
  });
  if (!approval.approved) return {ok: false, status: 'approval-required', approval: approval.approval};

  if (packageValue.delivery.kind === 'sandbox') {
    return sendToTabChannel(tabId, 'jangolova.cymonkey.sandbox', 'mount', input);
  }

  await sendToTab(tabId, 'describe', {});
  await browser.scripting.executeScript({
    target: {tabId},
    files: [`augmentations/${packageValue.description.id}/${packageValue.delivery.entrypoint}`],
    world: 'ISOLATED',
  } as unknown as Parameters<typeof browser.scripting.executeScript>[0]);
  return sendToTabChannel(tabId, 'jangolova.cymonkey.augmentation-runtime', 'mount', {
    ...input,
    package: packageValue.description.id,
    augmentationId,
  });
}

export async function unmountReviewedAugmentation(input: Record<string, unknown>) {
  const packageValue = await requireBrowserPackage(input.package, []);
  const augmentationId = requireScopedIdentifier(input.augmentationId, 'augmentationId');
  const tabId = requireTabID(await targetTab(input.target));
  const channel = packageValue.delivery.kind === 'sandbox'
    ? 'jangolova.cymonkey.sandbox'
    : 'jangolova.cymonkey.augmentation-runtime';
  return sendToTabChannel(tabId, channel, 'unmount', {...input, augmentationId});
}

function safeOrigin(value?: string) {
  if (!value) return 'unknown';
  try { return new URL(value).origin; } catch { return 'unknown'; }
}
