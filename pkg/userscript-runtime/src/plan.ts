import type { BrowserRegistration, UserScriptManifest } from './types.js';

export function registrationID(id: string) {
  return `jg-us-${id}`;
}

export function registrationWorldID(id: string) {
  return `jangolova-${id}`;
}

export function registrationPlan(value: UserScriptManifest): BrowserRegistration {
  return {
    id: registrationID(value.metadata.id),
    matches: [...value.spec.matches],
    excludeMatches: value.spec.excludeMatches.length ? [...value.spec.excludeMatches] : undefined,
    js: [{code: value.source.code}],
    runAt: value.spec.runAt,
    world: value.spec.world,
    ...(value.spec.world === 'USER_SCRIPT' ? {worldId: registrationWorldID(value.metadata.id)} : {}),
    allFrames: value.spec.allFrames,
  };
}

export function publicDescription(value: UserScriptManifest) {
  return {
    apiVersion: value.apiVersion,
    kind: value.kind,
    metadata: structuredClone(value.metadata),
    spec: structuredClone(value.spec),
    source: {origin: value.source.origin, bytes: new TextEncoder().encode(value.source.code).byteLength},
  };
}

export async function sourceRevision(source: string): Promise<`sha256:${string}`> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(source));
  return `sha256:${[...new Uint8Array(digest)].map((value) => value.toString(16).padStart(2, '0')).join('')}`;
}
