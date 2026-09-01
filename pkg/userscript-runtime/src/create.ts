import { sourceRevision } from './plan.js';
import { validateManifest } from './validate.js';
import { userscriptProtocolVersion, type ExecutionWorld, type RunAt, type UserScriptManifest } from './types.js';

export type UserScriptDraft = {
  id: string;
  name: string;
  matches: string[];
  code: string;
  namespace?: string;
  version?: string;
  description?: string;
  excludeMatches?: string[];
  runAt?: RunAt;
  world?: ExecutionWorld;
  allFrames?: boolean;
  enabled?: boolean;
  updateUrl?: string;
};

/**
 * Creates the only metadata block accepted for an agent-authored userscript.
 * This prepares a reviewable manifest; it never registers or executes code.
 */
export async function createManifest(draft: UserScriptDraft): Promise<UserScriptManifest> {
  if (draft.code.includes('==UserScript==')) throw new Error('userscript draft code must not contain a metadata block');
  const spec = {
    matches: [...draft.matches],
    excludeMatches: [...(draft.excludeMatches ?? [])],
    runAt: draft.runAt ?? 'document_idle' as RunAt,
    world: draft.world ?? 'USER_SCRIPT' as ExecutionWorld,
    allFrames: draft.allFrames ?? false,
    grants: ['none'],
    enabled: draft.enabled ?? true,
    ...(draft.updateUrl ? {updateUrl: draft.updateUrl} : {}),
  };
  const metadata = {
    id: draft.id,
    name: draft.name,
    ...(draft.namespace ? {namespace: draft.namespace} : {}),
    ...(draft.version ? {version: draft.version} : {}),
    ...(draft.description ? {description: draft.description} : {}),
  };
  const code = `${metadataBlock(metadata, spec)}\n${managedRuntimePrelude(draft.id, spec.world)}\n${draft.code}`;
  const manifest: UserScriptManifest = {
    apiVersion: userscriptProtocolVersion,
    kind: 'UserScript',
    metadata: {...metadata, revision: await sourceRevision(code)},
    spec,
    source: {origin: 'user', code},
  };
  return validateManifest(manifest, {allowMainWorld: true});
}

function managedRuntimePrelude(id: string, world: ExecutionWorld) {
  if (world !== 'USER_SCRIPT') return '';
  return `;(() => {
  const runtime = globalThis.browser?.runtime ?? globalThis.chrome?.runtime;
  if (!runtime?.connect) return;
  const scriptId = ${JSON.stringify(id)};
  const handlers = new Map();
  const port = runtime.connect({name: 'cymonkey.userscript/v1alpha1'});
  const send = (value) => { try { port.postMessage({channel: 'cymonkey.userscript/v1alpha1', scriptId, ...value}); } catch {} };
  const bridge = Object.freeze({
    register(actions) {
      if (!actions || typeof actions !== 'object' || Array.isArray(actions)) throw new Error('userscript actions must be an object');
      for (const [name, handler] of Object.entries(actions)) {
        if (!/^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/.test(name) || typeof handler !== 'function') throw new Error('userscript action must have a valid name and function');
        handlers.set(name, handler);
      }
      send({type: 'registered', actions: [...handlers.keys()].sort()});
    },
    emit(type, data = {}) {
      if (typeof type !== 'string' || !/^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/.test(type) || !data || typeof data !== 'object' || Array.isArray(data)) throw new Error('userscript event is invalid');
      send({type: 'event', eventType: type, data});
    },
  });
  const cymonkey = globalThis.cymonkey && typeof globalThis.cymonkey === 'object' ? globalThis.cymonkey : {};
  Object.defineProperty(cymonkey, 'jangolova', {value: bridge, configurable: false, writable: false});
  Object.defineProperty(globalThis, 'cymonkey', {value: cymonkey, configurable: false, writable: false});
  port.onMessage.addListener(async (message) => {
    if (!message || message.channel !== 'cymonkey.userscript/v1alpha1' || message.type !== 'call' || typeof message.requestId !== 'string') return;
    const request = message.request;
    const name = request?.method === 'act' ? request.params?.name : undefined;
    const input = request?.method === 'act' && request.params?.input && typeof request.params.input === 'object' && !Array.isArray(request.params.input) ? request.params.input : {};
    const handler = typeof name === 'string' ? handlers.get(name) : undefined;
    if (!handler) return send({type: 'response', requestId: message.requestId, error: 'userscript action is not registered'});
    try { send({type: 'response', requestId: message.requestId, result: await handler(input)}); }
    catch (error) { send({type: 'response', requestId: message.requestId, error: error instanceof Error ? error.message : String(error)}); }
  });
  send({type: 'connected'});
})();`;
}

function metadataBlock(metadata: Omit<UserScriptManifest['metadata'], 'revision'>, spec: Omit<UserScriptManifest['spec'], 'grants'> & {grants: string[]}) {
  const lines = [
    '// ==UserScript==',
    directive('name', metadata.name),
    metadata.namespace ? directive('namespace', metadata.namespace) : null,
    metadata.version ? directive('version', metadata.version) : null,
    metadata.description ? directive('description', metadata.description) : null,
    ...spec.matches.map((value) => directive('match', value)),
    ...spec.excludeMatches.map((value) => directive('exclude-match', value)),
    directive('grant', 'none'),
    directive('run-at', spec.runAt.replaceAll('_', '-')),
    spec.world === 'MAIN' ? directive('inject-into', 'page') : null,
    spec.updateUrl ? directive('updateURL', spec.updateUrl) : null,
    '// ==/UserScript==',
  ];
  return lines.filter((value): value is string => value !== null).join('\n');
}

function directive(name: string, value: string) {
  if (!value || /[\r\n]/.test(value)) throw new Error(`userscript ${name} must be a non-empty single-line value`);
  return `// @${name} ${value}`;
}
