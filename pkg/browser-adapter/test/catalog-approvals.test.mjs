import assert from 'node:assert/strict';
import test from 'node:test';
import {createApprovalManager, createAugmentationManager, createControlGate, createPackageCatalog} from '../dist/index.js';

function storage() {
  const values = new Map();
  return {
    get: async (key) => Object.fromEntries((Array.isArray(key) ? key : [key]).map((item) => [item, values.get(item)])),
    set: async (items) => { for (const [key, value] of Object.entries(items)) values.set(key, value); },
  };
}

test('package and userscript approvals bind to the reviewed scope and are single use', async () => {
  let time = 1000;
  let next = 0;
  const manager = createApprovalManager({storage: storage(), now: () => time, newId: () => `approval-${++next}`});
  const scope = {kind: 'package', subjectId: 'threejs', revision: 'sha256:one', targetId: 'tab-4', origin: 'https://example.com', permissions: ['camera']};
  const requested = await manager.request({...scope, token: 'must-not-store'});
  assert.equal(requested.approved, false);
  assert.equal(JSON.stringify(requested.approval).includes('must-not-store'), false);
  assert.equal((await manager.list()).length, 1);
  await assert.rejects(manager.request({...scope, origin: 'https://elsewhere.example'}, requested.approval.id), /does not match/);
  await manager.resolve(requested.approval.id, 'approve');
  assert.deepEqual(await manager.request(scope, requested.approval.id), {approved: true});
  await assert.rejects(manager.request(scope, requested.approval.id), /does not match/);
  const second = await manager.request(scope);
  time += 5 * 60 * 1000 + 1;
  await assert.rejects(manager.resolve(second.approval.id, 'approve'), /no longer pending/);
});

test('catalog loads only reviewed package manifests and selects a compatible delivery', async () => {
  const manifest = {
    apiVersion: 'cymonkey.browser-package/v1alpha1', kind: 'BrowserAugmentationPackage',
    metadata: {id: 'reader', name: 'Reader', version: '1.0.0'},
    spec: {deliveries: [{kind: 'augmentation-package', entrypoint: 'content.js', browsers: ['firefox']}], permissions: [], capabilities: ['reader.mount']},
  };
  const registry = {apiVersion: 'cymonkey.browser-package/v1alpha1', kind: 'BrowserPackageRegistry', packages: [{id: 'reader', manifest: 'augmentations/reader/manifest.json'}]};
  const load = async (path) => path === 'augmentations/registry.json' ? registry : manifest;
  const firefox = createPackageCatalog({load, browser: 'firefox'});
  assert.equal((await firefox.require('reader')).delivery.entrypoint, 'content.js');
  await assert.rejects(firefox.require('unlisted'), /not reviewed/);
  await assert.rejects(firefox.require('reader', ['camera']), /undeclared permission/);
  await assert.rejects(createPackageCatalog({load, browser: 'chrome'}).require('reader'), /no chrome delivery/);
  await assert.rejects(createPackageCatalog({load: async (path) => path.endsWith('registry.json') ? registry : {...manifest, metadata: {...manifest.metadata, id: 'wrong'}}, browser: 'firefox'}).list(), /does not match/);
});

test('control gate defaults to deny and audits only resolved metadata', async () => {
  const events = [];
  const gate = createControlGate({audit: (event) => { events.push(event); }});
  const context = {caller: 'trusted-host', capability: 'overlay.mount', effect: 'write', origin: 'https://example.com', augmentationId: 'reader'};
  await assert.rejects(gate.run(context, async () => { throw new Error('must not run'); }), /denied/);
  gate.replace({version: 1, defaultDecision: 'deny', rules: [
    {id: 'allow-reader', decision: 'allow', capabilities: ['overlay.mount'], augmentationIds: ['reader'], origins: ['https://example.com']},
  ]});
  assert.equal(await gate.run(context, async () => 'mounted'), 'mounted');
  assert.deepEqual(events.map((event) => event.phase), ['requested', 'denied', 'requested', 'succeeded']);
  assert.equal(JSON.stringify(events).includes('must not run'), false);
  await assert.rejects(gate.run({...context, augmentationId: 'other'}, async () => 'unexpected'), /denied/);
});

test('augmentation mount uses reviewed package, exact approval scope, and caller-owned routing', async () => {
  const approval = createApprovalManager({storage: storage(), newId: () => 'approval-one'});
  const injections = [];
  const messages = [];
  const manager = createAugmentationManager({
    catalog: {require: async () => ({description: {id: 'reader', version: '1.0.0'}, delivery: {kind: 'augmentation-package', entrypoint: 'content.js'}, permissions: []})},
    approvals: approval,
    tabs: {
      resolve: async () => ({id: 4, url: 'https://example.com/article'}),
      send: async (_, channel, method, payload) => { messages.push({channel, method, payload}); return {mounted: true}; },
    },
    browser: {scripts: {execute: async (...args) => { injections.push(args); }}},
  });
  const input = {augmentationId: 'reader-one', package: 'reader', configuration: {token: 'secret'}};
  const pending = await manager.mount(input);
  assert.equal(pending.status, 'approval-required');
  assert.equal(JSON.stringify(pending.approval).includes('secret'), false);
  await approval.resolve(pending.approval.id, 'approve');
  await assert.rejects(manager.mount({...input, augmentationId: 'reader-two', approvalId: pending.approval.id}), /does not match/);
  assert.deepEqual(await manager.mount({...input, approvalId: pending.approval.id}), {mounted: true});
  assert.deepEqual(injections, [['reader', 4, ['augmentations/reader/content.js']]]);
  assert.equal(messages[0].channel, 'cymonkey.jangolova.augmentation-runtime');
});
