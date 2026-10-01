import assert from 'node:assert/strict';
import test from 'node:test';
import {createExtensionManager} from '../dist/index.js';

const extension = (id, overrides = {}) => ({
  id, name: id, version: '1.0', enabled: true, type: 'extension',
  installType: 'normal', permissions: ['tabs'], hostPermissions: ['https://example.com/*'],
  mayDisable: true, ...overrides,
});

function fixture() {
  const entries = new Map([
    ['a', extension('a')],
    ['b', extension('b', {enabled: false, mayEnable: false})],
    ['theme', extension('theme', {type: 'theme'})],
  ]);
  const calls = [];
  const native = {
    async getAll() { return [...entries.values()]; },
    async get(id) { const item = entries.get(id); if (!item) throw Error('missing'); return item; },
    async setEnabled(id, enabled) { calls.push(['setEnabled', id, enabled]); entries.set(id, {...entries.get(id), enabled}); },
    async uninstall(id) { calls.push(['uninstall', id]); entries.delete(id); },
  };
  return {native, calls};
}

test('other extensions can build an inventory and lifecycle UI with the library', async () => {
  const {native, calls} = fixture();
  const manager = createExtensionManager(native);
  assert.deepEqual(manager.capabilities(), {
    list: true, describe: true, setEnabled: true, uninstall: true,
    prepare: false, install: false, events: false,
  });
  assert.deepEqual((await manager.list()).map((item) => item.id), ['a', 'b']);
  assert.deepEqual(await manager.setEnabled('a', false), {
    id: 'a', name: 'a', version: '1.0', enabled: false, installType: 'normal',
    permissions: ['tabs'], hostPermissions: ['https://example.com/*'], mayDisable: true,
  });
  await assert.rejects(manager.setEnabled('b', true), /cannot be enabled/);
  await assert.rejects(manager.describe('theme'), /not an extension/);
  await assert.rejects(manager.describe('../bad'), /valid browser extension ID/);
  assert.deepEqual(await manager.uninstall('a'), {uninstalled: 'a'});
  assert.deepEqual(calls, [['setEnabled', 'a', false], ['uninstall', 'a']]);
});

test('install and packaging remain an optional host adapter', async () => {
  const {native} = fixture();
  const calls = [];
  const manager = createExtensionManager(native, {
    async prepare(source) { calls.push(['prepare', source]); return {revision: 'sha256:reviewed'}; },
    async install(request) { calls.push(['install', request]); return {status: 'awaiting-browser-action'}; },
  });
  assert.equal(manager.capabilities().install, true);
  assert.deepEqual(await manager.prepare('/extension.zip'), {revision: 'sha256:reviewed'});
  assert.deepEqual(await manager.install({source: '/extension.zip', revision: 'sha256:reviewed'}), {status: 'awaiting-browser-action'});
  assert.deepEqual(calls, [
    ['prepare', '/extension.zip'],
    ['install', {source: '/extension.zip', revision: 'sha256:reviewed'}],
  ]);
});

test('watch reports browser changes and can be disposed', () => {
  const {native} = fixture();
  const listeners = new Set();
  native.onEnabled = {
    addListener(listener) { listeners.add(listener); },
    removeListener(listener) { listeners.delete(listener); },
  };
  const manager = createExtensionManager(native);
  const received = [];
  const stop = manager.watch((event) => received.push(event));
  for (const listener of listeners) listener(extension('changed'));
  assert.deepEqual(received.map((event) => [event.type, event.extension.id]), [['enabled', 'changed']]);
  stop();
  assert.equal(listeners.size, 0);
});
