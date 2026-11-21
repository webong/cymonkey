import assert from 'node:assert/strict';
import test from 'node:test';
import {createTabRouter} from '../dist/index.js';

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
  await assert.rejects(protectedRouter.send({}, 'example.channel', 'hello'), /Cannot attach to this tab URL/);
});

test('tab router refuses an oversized message before invoking the browser API', async () => {
  let sends = 0;
  const router = createTabRouter({
    tabs: {
      query: async () => [],
      get: async (id) => ({id, url: 'https://example.com'}),
      sendMessage: async () => { sends += 1; return {ok: true}; },
    },
  });
  await assert.rejects(
    router.send({tabId: 7}, 'example.channel', 'large', {body: 'x'.repeat(1024 * 1024)}),
    (error) => error?.code === 'too_large',
  );
  assert.equal(sends, 0);
});
