export type SessionDescription = {type: 'offer' | 'answer'; sdp: string};
export type MediaBrokerDependencies = {
  exists: () => Promise<boolean>;
  create: () => Promise<void>;
  close: () => Promise<void>;
  send: (message: {method: 'camera.open' | 'camera.close'; sessionKey: string; offer?: SessionDescription}) => Promise<unknown>;
};

export function createMediaBrokerController(dependencies: MediaBrokerDependencies) {
  if (!dependencies?.exists || !dependencies.create || !dependencies.close || !dependencies.send) throw new Error('media broker dependencies are incomplete');
  let creating: Promise<void> | null = null;
  const ensure = async () => {
    if (await dependencies.exists()) return;
    creating ??= dependencies.create().finally(() => { creating = null; });
    await creating;
  };
  const key = (tabId: number, sessionId: string) => {
    if (!Number.isInteger(tabId) || tabId < 0 || typeof sessionId !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(sessionId)) {
      throw new Error('media session target is invalid');
    }
    return `${tabId}:${sessionId}`;
  };
  return {
    async open(tabId: number, sessionId: string, offer: SessionDescription) {
      const sessionKey = key(tabId, sessionId);
      const checked = description(offer, 'offer');
      await ensure();
      const response = await dependencies.send({method: 'camera.open', sessionKey, offer: checked});
      if (!record(response) || response.ok !== true) throw new Error('camera broker failed');
      return {answer: description(response.answer, 'answer')};
    },
    async close(tabId: number, sessionId: string) {
      const sessionKey = key(tabId, sessionId);
      if (!await dependencies.exists()) return {closed: false};
      const response = await dependencies.send({method: 'camera.close', sessionKey});
      if (!record(response) || response.ok !== true) throw new Error('camera broker failed');
      if (response.activeSessions === 0) await dependencies.close();
      return {closed: response.closed === true};
    },
  };
}

function description(value: unknown, expected: 'offer' | 'answer'): SessionDescription {
  if (!record(value) || value.type !== expected || typeof value.sdp !== 'string' || value.sdp.length > 1_000_000) throw new Error(`media broker ${expected} is invalid`);
  return {type: expected, sdp: value.sdp};
}
function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
