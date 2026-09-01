import { publishCymonkeyEngineEvent } from './events';
import { requireTabID, sendToTabChannel, targetTab } from './tabs';

// A bundle may provide a private runtime endpoint without the extension knowing
// which JavaScript library it uses. The bundle owns the listener and filters by
// augmentationId; the extension only provides authenticated routing.
export async function callCymonkeyEngine(request: unknown, target: unknown = {}, augmentationId?: unknown, delivery: unknown = 'page-runtime', sandboxId?: unknown) {
  const tabId = requireTabID(await targetTab(target));
  if (delivery === 'sandbox') {
    if (typeof augmentationId !== 'string' || !augmentationId) throw new Error('sandbox delivery requires augmentationId');
    if (typeof sandboxId !== 'string' || !sandboxId) throw new Error('sandbox delivery requires sandboxId');
    const value = await sendToTabChannel(tabId, 'cymonkey.jangolova.sandbox', 'call', {augmentationId, id: sandboxId, request});
    await publishCymonkeyEngineEvent('request.completed', {tabId, augmentationId, sandboxId, runtime: 'sandbox'}, tabId);
    return value;
  }
  if (delivery === 'augmentation-package') {
    if (typeof augmentationId !== 'string' || !augmentationId) throw new Error('augmentation-package delivery requires augmentationId');
    const value = await sendToTabChannel(tabId, 'cymonkey.jangolova.augmentation-runtime', 'dispatch', { augmentationId, request });
    await publishCymonkeyEngineEvent('request.completed', { tabId, augmentationId, runtime: 'augmentation-package' }, tabId);
    return value;
  }
  if (delivery !== 'page-runtime') throw new Error('unsupported Cymonkey runtime delivery');
  const results = await browser.scripting.executeScript({
    target: { tabId },
    world: 'MAIN',
    func: async (engineRequest: unknown) => {
      const symbol = Symbol.for('cymonkey.jangolova.runtime');
      const runtime = (globalThis as Record<PropertyKey, unknown>)[symbol] as { dispatch?(request: unknown): unknown } | undefined;
      if (!runtime?.dispatch) throw new Error('no explicitly installed Cymonkey runtime was found');
      return runtime.dispatch(engineRequest);
    },
    args: [request],
  } as unknown as Parameters<typeof browser.scripting.executeScript>[0]);
  const value = results[0]?.result;
  await publishCymonkeyEngineEvent('request.completed', {
    tabId, runtime: 'page-main-world', augmentationId: typeof augmentationId === 'string' ? augmentationId : undefined,
  }, tabId);
  return value;
}
