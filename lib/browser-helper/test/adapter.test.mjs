import assert from 'node:assert/strict';
import test from 'node:test';
import {createBrowserAdapter, decidePolicy, installEphemeralWebStorage, validateBrowserPackage} from '../dist/index.js';

function fixture() {
  const records = new Map();
  const calls = [];
  const adapter = createBrowserAdapter({
    storage: {
      async get(keys) { return Object.fromEntries((Array.isArray(keys) ? keys : [keys]).map((key) => [key, records.get(key)])); },
      async set(values) { for (const [key, value] of Object.entries(values)) records.set(key, value); },
    },
    scripting: {
      async registerContentScripts(scripts) { calls.push(['register', scripts]); },
      async unregisterContentScripts(options) { calls.push(['unregister', options]); },
      async executeScript(options) { calls.push(['execute', options]); return [{frameId: 0}]; },
      async insertCSS(options) { calls.push(['insertCSS', options]); },
      async removeCSS(options) { calls.push(['removeCSS', options]); },
    },
    network: {
      async updateDynamicRules(options) { calls.push(['rules', options]); },
    },
    now: () => '2026-01-01T00:00:00.000Z',
  });
  return {adapter, calls};
}

test('scripts and storage are bounded by an owner supplied by the integrating extension', async () => {
  const {adapter, calls} = fixture();
  assert.deepEqual(adapter.capabilities(), {storage: true, events: true, scripts: true, networkRules: true});
  await adapter.storage.set('reader', {mode: 'dark'});
  assert.deepEqual(await adapter.storage.get('reader', ['mode']), {mode: 'dark'});
  assert.deepEqual(await adapter.storage.get('other', ['mode']), {mode: null});
  const registration = {id: 'main', matches: ['https://example.com/*'], files: ['augmentations/reader/content.js']};
  assert.deepEqual(await adapter.scripts.register('reader', [registration]), {ids: ['cm-6-reader-main']});
  await assert.rejects(adapter.scripts.register('reader', [{...registration, files: ['augmentations/other/content.js']}]), /packaged below/);
  assert.deepEqual(await adapter.scripts.execute('reader', 3, registration.files), {tabId: 3, frames: [0]});
  await adapter.scripts.style('reader', 3, 'body { color: red; }', 'insert');
  await adapter.scripts.unregister('reader', ['main']);
  assert.deepEqual(calls.map(([name]) => name), ['register', 'execute', 'insertCSS', 'unregister']);
  const events = await adapter.events.read();
  assert.deepEqual(events.events.map((event) => event.type), ['storage.updated', 'script.registered', 'script.executed', 'style.inserted', 'script.unregistered']);
});

test('script ids do not collide across owners with punctuation or separators', async () => {
  const {adapter} = fixture();
  const results = await Promise.all([
    adapter.scripts.register('a.b', [{id: 'c', matches: ['https://example.com/*'], files: ['augmentations/a.b/content.js']}]),
    adapter.scripts.register('a-b', [{id: 'c', matches: ['https://example.com/*'], files: ['augmentations/a-b/content.js']}]),
    adapter.scripts.register('a', [{id: 'b-c', matches: ['https://example.com/*'], files: ['augmentations/a/content.js']}]),
    adapter.scripts.register('a-b', [{id: 'c2', matches: ['https://example.com/*'], files: ['augmentations/a-b/content.js']}]),
  ]);
  assert.equal(new Set(results.flatMap((result) => result.ids)).size, 4);
});

test('network rules cannot be taken over by another owner', async () => {
  const {adapter, calls} = fixture();
  await adapter.network.install('alpha', [{id: 7, action: {type: 'block'}}]);
  await assert.rejects(adapter.network.install('beta', [{id: 7, action: {type: 'redirect'}}]), /another owner/);
  await assert.rejects(adapter.network.remove('beta', [7]), /not owned/);
  await adapter.network.remove('alpha', [7]);
  assert.equal(calls.filter(([name]) => name === 'rules').length, 2);
});

test('policy and package checks are reusable without browser globals', () => {
  const policy = {
    version: 1, defaultDecision: 'deny', rules: [
      {id: 'allow-read', decision: 'allow', effects: ['read']},
      {id: 'deny-private', decision: 'deny', origins: ['https://private.example']},
    ],
  };
  assert.deepEqual(decidePolicy(policy, {caller: 'tool', capability: 'extension.list', effect: 'read', origin: 'https://private.example'}), {decision: 'deny', ruleId: 'deny-private'});
  assert.deepEqual(decidePolicy(policy, {caller: 'tool', capability: 'extension.list', effect: 'read', origin: 'https://public.example'}), {decision: 'allow', ruleId: 'allow-read'});
  const manifest = {
    apiVersion: 'cymonkey.browser-package/v1alpha1', kind: 'BrowserAugmentationPackage',
    metadata: {id: 'reader', name: 'Reader', version: '1.0.0'},
    spec: {deliveries: [{kind: 'augmentation-package', entrypoint: 'content.js', browsers: ['chrome']}], permissions: [], capabilities: []},
  };
  assert.equal(validateBrowserPackage(manifest).id, 'reader');
  assert.throws(() => validateBrowserPackage({...manifest, spec: {...manifest.spec, permissions: ['cookies']}}), /unsupported/);
});

test('opaque sandbox storage stays in memory', () => {
  const target = {};
  for (const name of ['localStorage', 'sessionStorage']) {
    Object.defineProperty(target, name, {configurable: true, get() { throw new DOMException('opaque origin', 'SecurityError'); }});
  }
  installEphemeralWebStorage(target);
  target.sessionStorage.setItem('key', 42);
  assert.equal(target.sessionStorage.getItem('key'), '42');
  assert.equal(target.localStorage.length, 0);
});
