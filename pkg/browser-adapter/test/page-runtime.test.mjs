import assert from 'node:assert/strict';
import test from 'node:test';
import {connectSandboxHost, createAugmentationRuntimeHost, createDOMPageRuntime, createEngineRouter, createUserscriptConnectionHub, installPageBridgeClient, installPageBridgeReceiver, startSandboxGuest} from '../dist/index.js';

test('augmentation runtime host mounts, calls, and unmounts caller factories', async () => {
  const events = [];
  let removed = false;
  const host = createAugmentationRuntimeHost({
    factories: new Map([['reader', async ({packageId}) => ({packageId, dispatch: (request) => ({request}), unmount: () => { removed = true; }})]]),
    onEvent: (event) => { events.push(event.type); },
  });
  await host.mount({packageId: 'reader', augmentationId: 'article'});
  assert.deepEqual(await host.call('article', {method: 'describe'}), {request: {method: 'describe'}});
  await assert.rejects(host.mount({packageId: 'reader', augmentationId: 'article'}), /already exists/);
  await assert.rejects(host.unmount('article', 'other'), /different package/);
  await host.unmount('article', 'reader');
  assert.equal(removed, true);
  assert.deepEqual(events, ['augmentation.mounted', 'augmentation.unmounted']);
});

test('page bridge exposes only allowed page actions', async () => {
  const listeners = new Set();
  const target = {
    addEventListener(_type, listener) { listeners.add(listener); },
    removeEventListener(_type, listener) { listeners.delete(listener); },
    postMessage(data) { queueMicrotask(() => { for (const listener of listeners) listener({source: target, data}); }); },
  };
  const stop = installPageBridgeReceiver(target, {
    allowedActions: new Set(['document.query']),
    dispatch: async (method, params) => ({method, params}),
  });
  const client = installPageBridgeClient(target);
  assert.deepEqual(await client.api.act('document.query', {selector: 'body'}), {method: 'act', params: {name: 'document.query', input: {selector: 'body'}}});
  await assert.rejects(client.api.act('storage.set', {values: {secret: true}}), /privileged action/);
  client.dispose();
  stop();
  assert.equal(target.cymonkey.jangolova, undefined);
});

test('DOM page runtime bounds queries and owns overlay lifecycle', () => {
  const createElement = (tagName) => {
    const item = {
      tagName: tagName.toUpperCase(), id: '', classList: [], textContent: '', dataset: {}, children: [], removed: false,
      append(child) { this.children.push(child); },
      replaceChildren() { this.children = []; },
      remove() { this.removed = true; },
      attachShadow() { return createElement('shadow'); },
    };
    return item;
  };
  const root = createElement('html');
  const nodes = Array.from({length: 3}, (_, index) => ({tagName: 'P', id: `p${index}`, classList: ['text'], textContent: '  Article   text  '}));
  const doc = {title: 'Article', readyState: 'complete', documentElement: root, createElement, querySelectorAll: () => nodes};
  const page = createDOMPageRuntime({document: doc, url: () => 'https://example.com/a', driver: 'webextension'});
  assert.equal(page.act('document.query', {selector: 'p', limit: 2}).truncated, true);
  assert.deepEqual(page.act('document.query', {selector: 'p', limit: 1}).matches[0], {tag: 'p', id: 'p0', classes: ['text'], text: 'Article text'});
  page.act('overlay.mount', {id: 'reader', html: '<p>Reader</p>'});
  assert.deepEqual(page.describe().surfaces[0].properties.overlays, ['reader']);
  assert.throws(() => page.act('overlay.mount', {id: 'reader'}), /already exists/);
  page.act('overlay.patch', {id: 'reader', css: 'p { color: red }'});
  assert.deepEqual(page.events({after: '1'}).events.map((event) => event.type), ['overlay.patched']);
  page.act('overlay.unmount', {id: 'reader'});
  assert.equal(root.children[0].removed, true);
  page.dispose();
});

test('userscript connection hub authorizes identity and routes calls', async () => {
  const messageListeners = new Set();
  const disconnectListeners = new Set();
  const posts = [];
  const port = {
    sender: {tab: {id: 7}},
    onMessage: {addListener: (listener) => messageListeners.add(listener), removeListener: (listener) => messageListeners.delete(listener)},
    onDisconnect: {addListener: (listener) => disconnectListeners.add(listener), removeListener: (listener) => disconnectListeners.delete(listener)},
    postMessage: (message) => posts.push(message),
  };
  const events = [];
  const hub = createUserscriptConnectionHub({authorize: (id) => id === 'reader', onEvent: (event) => events.push(event.type)});
  const detach = hub.attach(port);
  for (const listener of messageListeners) listener({channel: 'cymonkey.userscript/v1alpha1', scriptId: 'other', type: 'registered'});
  assert.equal(hub.describe('other', 7).status, 'unavailable');
  for (const listener of messageListeners) listener({channel: 'cymonkey.userscript/v1alpha1', scriptId: 'reader', type: 'registered', actions: ['summarize', 'summarize']});
  assert.deepEqual(hub.describe('reader', 7).actions, ['summarize']);
  const answer = hub.call('reader', 7, {method: 'act', params: {name: 'summarize', input: {length: 2}}});
  assert.equal(posts[0].request.params.name, 'summarize');
  for (const listener of messageListeners) listener({channel: 'cymonkey.userscript/v1alpha1', scriptId: 'reader', type: 'response', requestId: posts[0].requestId, result: {ok: true}});
  assert.deepEqual(await answer, {ok: true});
  assert.deepEqual(events, ['connected', 'registered']);
  detach();
  assert.equal(hub.describe('reader', 7).status, 'unavailable');
  assert.equal(messageListeners.size, 0);
  assert.equal(disconnectListeners.size, 0);
});

test('sandbox channel binds guest source and nonce before transferring a port', async () => {
  const hostListeners = new Set();
  const guestListeners = new Set();
  const host = {
    addEventListener(_type, listener) { hostListeners.add(listener); },
    removeEventListener(_type, listener) { hostListeners.delete(listener); },
    postMessage(data) { queueMicrotask(() => { for (const listener of hostListeners) listener({source: guest, data}); }); },
  };
  const guest = {
    addEventListener(_type, listener) { guestListeners.add(listener); },
    removeEventListener(_type, listener) { guestListeners.delete(listener); },
    postMessage(data, _origin, ports = []) { queueMicrotask(() => { for (const listener of guestListeners) listener({source: host, data, ports}); }); },
  };
  // Each postMessage reaches its target and carries the opposite window as source.
  let received;
  const stop = startSandboxGuest({
    guest, parent: host, id: 'camera', nonce: 'nonce123', load: () => undefined,
    connect: (context, port) => { received = {context, port}; },
  });
  const promise = connectSandboxHost({
    host, guest, id: 'camera', nonce: 'nonce123',
    context: {augmentationId: 'overlay', configuration: {mode: 'preview'}},
    createChannel: () => new MessageChannel(),
  });
  for (const listener of hostListeners) listener({source: {}, data: {channel: 'cymonkey.jangolova.sandbox.ready', id: 'camera', nonce: 'nonce123', status: 'ready'}});
  const port = await promise;
  assert.deepEqual(received.context, {augmentationId: 'overlay', configuration: {mode: 'preview'}});
  assert.equal(port.constructor.name, 'MessagePort');
  port.close();
  received.port.close();
  stop();
  assert.equal(hostListeners.size, 0);
  assert.equal(guestListeners.size, 0);
});

test('engine router selects caller-owned runtime delivery', async () => {
  const sent = [];
  const completed = [];
  const router = createEngineRouter({
    tabs: {
      resolve: async () => ({id: 5}),
      send: async (target, channel, method, params) => { sent.push({target, channel, method, params}); return 'sent'; },
    },
    executeMainWorld: async (tabId, request) => ({tabId, request}),
    onComplete: (event) => completed.push(event.delivery),
  });
  assert.deepEqual(await router.call({request: {method: 'hello'}}), {tabId: 5, request: {method: 'hello'}});
  assert.equal(await router.call({request: {}, augmentationId: 'reader', delivery: 'augmentation-package'}), 'sent');
  assert.equal(sent[0].channel, 'cymonkey.jangolova.augmentation-runtime');
  assert.equal(await router.call({request: {}, augmentationId: 'reader', sandboxId: 'view', delivery: 'sandbox'}), 'sent');
  assert.equal(sent[1].channel, 'cymonkey.jangolova.sandbox');
  await assert.rejects(router.call({request: {}, delivery: 'sandbox'}), /requires valid/);
  assert.deepEqual(completed, ['page-runtime', 'augmentation-package', 'sandbox']);
});
