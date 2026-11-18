import assert from 'node:assert/strict';
import test from 'node:test';
import {createNativeUserscriptManager, createTabRouter} from '../dist/index.js';

function storage() {
  const values = new Map();
  return {
    get: async (key) => Object.fromEntries((Array.isArray(key) ? key : [key]).map((item) => [item, values.get(item)])),
    set: async (items) => { for (const [key, value] of Object.entries(items)) values.set(key, value); },
  };
}

test('native userscript lifecycle requires approval and reconciles only owned registrations', async () => {
  const nativeIDs = new Set(['other-extension-script']);
  let approved = false;
  const native = {
    getScripts: async () => [...nativeIDs].map((id) => ({id})),
    register: async (scripts) => { for (const item of scripts) nativeIDs.add(item.id); },
    update: async (scripts) => { for (const item of scripts) assert.ok(nativeIDs.has(item.id)); },
    unregister: async ({ids}) => { for (const id of ids) nativeIDs.delete(id); },
  };
  const manager = createNativeUserscriptManager({
    storage: storage(), native,
    validate: (value) => {
      if (value?.metadata?.revision !== `rev:${value?.source}`) throw new Error('source revision mismatch');
      return value;
    },
    describe: (value) => ({id: value.metadata.id, revision: value.metadata.revision, enabled: value.spec.enabled}),
    registration: (value) => ({id: `owned-${value.metadata.id}`, js: value.source}),
    registrationId: (id) => `owned-${id}`,
    ownsRegistrationId: (id) => id.startsWith('owned-'),
    authorize: async () => { if (!approved) throw new Error('approval required'); },
  });
  const script = {metadata: {id: 'reader', revision: 'rev:code'}, spec: {enabled: true}, source: 'code'};
  await assert.rejects(manager.install(script), /approval required/);
  approved = true;
  await manager.install(script);
  assert.deepEqual([...nativeIDs].sort(), ['other-extension-script', 'owned-reader']);
  assert.deepEqual(await manager.list(), [{id: 'reader', revision: 'rev:code', enabled: true}]);
  nativeIDs.delete('owned-reader');
  nativeIDs.add('owned-orphan');
  assert.deepEqual(await manager.reconcile(), {registered: 1, restored: 1, removed: 1});
  assert.deepEqual([...nativeIDs].sort(), ['other-extension-script', 'owned-reader']);
  await manager.setEnabled('reader', false);
  assert.equal(nativeIDs.has('owned-reader'), false);
  await manager.uninstall('reader');
  assert.deepEqual(await manager.list(), []);
});

test('tab router retries a missing receiver only after a safe HTTP(S) attach', async () => {
  const attached = [];
  let ready = false;
  const router = createTabRouter({
    tabs: {
      query: async () => [{id: 4, url: 'https://example.com'}],
      get: async (id) => ({id, url: 'https://example.com'}),
      sendMessage: async () => { if (!ready) throw new Error('Receiving end does not exist'); return {ok: true}; },
    },
    attach: async (id) => { attached.push(id); ready = true; },
  });
  assert.deepEqual(await router.send({}, 'example.channel', 'hello'), {ok: true});
  assert.deepEqual(attached, [4]);
  const protectedRouter = createTabRouter({
    tabs: {
      query: async () => [{id: 5, url: 'chrome://extensions'}],
      get: async (id) => ({id, url: 'chrome://extensions'}),
      sendMessage: async () => { throw new Error('Receiving end does not exist'); },
    },
    attach: async () => { throw new Error('must not run'); },
  });
  await assert.rejects(protectedRouter.send({}, 'example.channel', 'hello'), /HTTP\(S\)/);
});
