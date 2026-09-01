import {isRecord} from '../types';

type OffscreenAPI = {
  createDocument(options: {url: string; reasons: string[]; justification: string}): Promise<void>;
  closeDocument(): Promise<void>;
};

type RuntimeWithContexts = typeof browser.runtime & {
  getContexts?(filter: {contextTypes: string[]; documentUrls: string[]}): Promise<unknown[]>;
};

const documentPath = '/media-broker.html';
let creating: Promise<void> | null = null;

export async function dispatchMediaBroker(method: string, params: Record<string, unknown>, senderTabId?: number) {
  if (!Number.isInteger(senderTabId)) throw new Error('media broker requests must come from a target tab');
  const sessionId = identifier(params.sessionId, 'media session id');
  const sessionKey = `${senderTabId}:${sessionId}`;
  if (method === 'camera.open') {
    const offer = description(params.offer, 'offer');
    await ensureMediaBroker();
    const result = await browser.runtime.sendMessage({
      target: 'cymonkey.jangolova.media-broker.offscreen', method: 'camera.open',
      params: {sessionKey, offer},
    });
    if (!isRecord(result) || result.ok !== true) throw new Error(isRecord(result) ? String(result.error || 'camera broker failed') : 'camera broker did not respond');
    return {answer: description(result.answer, 'answer')};
  }
  if (method === 'camera.close') {
    if (!await mediaBrokerExists()) return {ok: true, closed: false};
    const result = await browser.runtime.sendMessage({
      target: 'cymonkey.jangolova.media-broker.offscreen', method: 'camera.close',
      params: {sessionKey},
    });
    if (!isRecord(result) || result.ok !== true) throw new Error(isRecord(result) ? String(result.error || 'camera broker failed') : 'camera broker did not respond');
    if (result.activeSessions === 0) await closeMediaBroker();
    return {ok: true, closed: result.closed === true};
  }
  throw new Error(`unsupported media broker method ${JSON.stringify(method)}`);
}

async function ensureMediaBroker() {
  const api = extensionAPI();
  if (await mediaBrokerExists()) return;
  if (!creating) {
    creating = api.offscreen.createDocument({
      url: documentPath.slice(1),
      reasons: ['USER_MEDIA', 'WEB_RTC'],
      justification: 'Acquire user-approved camera media outside target-site Permissions Policy and relay it to an isolated augmentation sandbox.',
    }).finally(() => { creating = null; });
  }
  await creating;
}

async function mediaBrokerExists() {
  const api = extensionAPI();
  const getContexts = api.runtime.getContexts;
  if (!getContexts) return false;
  const contexts = await getContexts.call(api.runtime, {
    contextTypes: ['OFFSCREEN_DOCUMENT'],
    documentUrls: [browser.runtime.getURL(documentPath)],
  });
  return contexts.length > 0;
}

async function closeMediaBroker() {
  const api = extensionAPI();
  if (await mediaBrokerExists()) await api.offscreen.closeDocument();
}

function extensionAPI() {
  const api = browser as typeof browser & {offscreen?: OffscreenAPI; runtime: RuntimeWithContexts};
  if (!api.offscreen) throw new Error('camera media broker is unavailable in this browser');
  return api as typeof api & {offscreen: OffscreenAPI};
}

function description(value: unknown, expectedType: 'offer' | 'answer') {
  if (!isRecord(value) || value.type !== expectedType || typeof value.sdp !== 'string' || value.sdp.length > 1_000_000) {
    throw new Error(`media broker ${expectedType} is invalid`);
  }
  return {type: expectedType, sdp: value.sdp};
}

function identifier(value: unknown, name: string) {
  if (typeof value !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value)) throw new Error(`${name} is invalid`);
  return value;
}
