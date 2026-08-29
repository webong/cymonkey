import type { CymonkeyEvent, EventQuery } from '../types';

const sequenceKey = 'jangolova.eventSequence';
const eventsKey = 'jangolova.events';
let eventChain = Promise.resolve<unknown>(undefined);

function eventStorage() {
  return browser.storage.session ?? browser.storage.local;
}

export function appendEvent(
  type: string,
  data: Record<string, unknown> = {},
  tabId?: number,
): Promise<{ accepted: true; id: string; tabId: number | null }> {
  const operation = async () => {
    const storage = eventStorage();
    const stored = await storage.get([sequenceKey, eventsKey]);
    const sequence = Number(stored[sequenceKey] || 0) + 1;
    const events = Array.isArray(stored[eventsKey])
      ? stored[eventsKey] as CymonkeyEvent[]
      : [];
    events.push({
      id: String(sequence),
      type,
      occurredAt: new Date().toISOString(),
      domain: 'computer',
      runtime: 'browser-dom',
      driver: 'webextension',
      data: tabId === undefined ? { ...data } : { ...data, tabId },
    });
    if (events.length > 256) events.splice(0, events.length - 256);
    await storage.set({ [sequenceKey]: sequence, [eventsKey]: events });
    return { accepted: true as const, id: String(sequence), tabId: tabId ?? null };
  };

  const result = eventChain.then(operation, operation);
  eventChain = result;
  return result;
}

export const publishCymonkeyEvent = (type: string, data: Record<string, unknown> = {}, tabId?: number) =>
  appendEvent(`cymonkey.${type}`, data, tabId);

export const publishCymonkeyEngineEvent = (type: string, data: Record<string, unknown> = {}, tabId?: number) =>
  appendEvent(`cymonkey-engine.${type}`, data, tabId);

export const publishAuditEvent = (phase: 'requested' | 'succeeded' | 'denied' | 'failed', data: Record<string, unknown>) =>
  appendEvent(`audit.control.${phase}`, data);

export async function readEvents(query: EventQuery = {}) {
  const cursor = Number.parseInt(query.after || '0', 10);
  if (!Number.isSafeInteger(cursor) || cursor < 0) {
    throw new Error('events.after must be a non-negative integer cursor');
  }
  const types = new Set(Array.isArray(query.types) ? query.types : []);
  const maximum = Math.min(Math.max(Number(query.limit) || 100, 1), 256);
  const stored = await eventStorage().get([sequenceKey, eventsKey]);
  const events = Array.isArray(stored[eventsKey])
    ? stored[eventsKey] as CymonkeyEvent[]
    : [];
  return {
    events: events
      .filter((event) => Number(event.id) > cursor && (types.size === 0 || types.has(event.type)))
      .slice(0, maximum),
    cursor: String(stored[sequenceKey] || 0),
  };
}
