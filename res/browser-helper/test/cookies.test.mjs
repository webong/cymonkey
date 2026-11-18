import assert from 'node:assert/strict';
import test from 'node:test';
import {createApprovalManager, createControlGate, createCookieExporter} from '../dist/index.js';

function storage() {
  const values = new Map();
  return {
    get: async (key) => ({[key]: values.get(key)}),
    set: async (items) => { for (const [key, value] of Object.entries(items)) values.set(key, value); },
  };
}

function fixture(overrides = {}) {
  const selection = {browser: 'firefox', profile: 'Personal', storeId: 'firefox-container-2', tabId: 7};
  const request = {selection, sites: ['https://example.com/account'], names: ['session']};
  const cookie = {
    name: 'session', value: 'private-cookie-value', domain: 'example.com', path: '/account',
    secure: true, httpOnly: true, hostOnly: true, session: true, sameSite: 'lax', storeId: selection.storeId,
    firstPartyDomain: '',
  };
  const calls = {permission: [], query: [], delivery: [], audit: []};
  const approvals = createApprovalManager({storage: storage(), newId: () => 'cookie-approval'});
  const gate = createControlGate({policy: {version: 1, defaultDecision: 'deny', rules: [
    {id: 'cookie-export', decision: 'allow', capabilities: ['cookies.export'], origins: ['https://example.com']},
  ]}, audit: (event) => { calls.audit.push(event); }});
  const cookies = {
    getAllCookieStores: async () => [{id: selection.storeId, tabIds: [7]}],
    getAll: async (details) => { calls.query.push(details); return [cookie]; },
  };
  const permissions = {
    request: (details) => { calls.permission.push(details); return Promise.resolve(true); },
    contains: async () => true,
  };
  const exporter = createCookieExporter({cookies, permissions, approvals, gate, caller: 'trusted-ui',
    selected: async () => selection, deliver: async (payload) => { calls.delivery.push(payload); }, ...overrides});
  return {selection, request, cookie, calls, approvals, exporter};
}

async function approve(f, request = f.request) {
  const pending = await f.exporter.prepare(request);
  assert.equal(pending.status, 'approval-required');
  await f.approvals.resolve(pending.approval.id, 'approve');
  const ready = await f.exporter.prepare(request, pending.approval.id);
  assert.equal(ready.status, 'ready');
  return ready.grant;
}

test('approved user click exports only selected HttpOnly cookies and redacts audit', async () => {
  const f = fixture();
  assert.deepEqual(f.exporter.availability(), {available: true});
  const grant = await approve(f);
  const exportPromise = f.exporter.exportFromUserGesture(grant);
  assert.equal(f.calls.permission.length, 1, 'permission request begins synchronously inside click handler');
  assert.deepEqual(await exportPromise, {status: 'delivered', count: 1, selection: f.selection});
  assert.deepEqual(f.calls.permission[0], {permissions: ['cookies'], origins: ['https://example.com/*']});
  assert.deepEqual(f.calls.query, [{url: 'https://example.com/account', name: 'session', storeId: f.selection.storeId}]);
  assert.equal(f.calls.delivery[0].format, 'cymonkey.webextension-cookies/v1');
  assert.deepEqual(f.calls.delivery[0].cookies, [f.cookie]);
  assert.equal(JSON.stringify(f.calls.audit).includes(f.cookie.value), false);
  assert.deepEqual(f.calls.audit.map((event) => event.phase), ['requested', 'succeeded']);
  assert.equal(JSON.stringify(await f.approvals.list()).includes(f.cookie.value), false);
  await assert.rejects(f.exporter.exportFromUserGesture(grant), /already used/);
});

test('site, name, selected store, partition, and first-party domain bind the query and output', async () => {
  const f = fixture({scopeSupport: {partitionKey: true, firstPartyDomain: true}, cookies: {
    getAllCookieStores: async () => [{id: 'firefox-container-2', tabIds: [7]}],
    getAll: async (details) => {
      f.calls.query.push(details);
      return [{...f.cookie, firstPartyDomain: 'news.example', partitionKey: {topLevelSite: 'https://news.example', hasCrossSiteAncestor: true}}];
    },
  }});
  const request = {...f.request, partition: {topLevelSite: 'https://news.example'}, firstPartyDomain: 'news.example'};
  const grant = await approve(f, request);
  await f.exporter.exportFromUserGesture(grant);
  assert.deepEqual(f.calls.query, [{url: 'https://example.com/account', name: 'session', storeId: f.selection.storeId,
    partitionKey: {topLevelSite: 'https://news.example'}, firstPartyDomain: 'news.example'}]);
  assert.deepEqual(f.calls.delivery[0].cookies[0].partitionKey, {topLevelSite: 'https://news.example', hasCrossSiteAncestor: true});
  assert.equal(f.calls.delivery[0].cookies[0].firstPartyDomain, 'news.example');
  assert.equal(f.calls.delivery[0].selection.storeId, 'firefox-container-2');
});

test('wrong store, changed selection, and out-of-scope cookies fail closed', async () => {
  const f = fixture({cookies: {
    getAllCookieStores: async () => [{id: 'firefox-container-2', tabIds: [7]}],
    getAll: async () => [{...f.cookie, storeId: 'different-container'}],
  }});
  const grant = await approve(f);
  await assert.rejects(f.exporter.exportFromUserGesture(grant), /out-of-scope/);
  assert.equal(f.calls.delivery.length, 0);

  const changed = fixture({selected: async () => ({...f.selection, profile: 'Other'})});
  await assert.rejects(changed.exporter.prepare(changed.request), /cookie store changed/);
  assert.equal(changed.calls.permission.length, 0);
});

test('ordinary pages and content scripts have no cookie export fallback', async () => {
  const f = fixture({cookies: undefined, permissions: undefined});
  assert.equal(f.exporter.availability().available, false);
  await assert.rejects(f.exporter.prepare(f.request), /privileged extension context/);
  assert.equal(f.calls.delivery.length, 0);
});

test('approval binds exact site and name scope before permission prompting', async () => {
  const f = fixture();
  const pending = await f.exporter.prepare(f.request);
  await f.approvals.resolve(pending.approval.id, 'approve');
  await assert.rejects(f.exporter.prepare({...f.request, names: ['other']}, pending.approval.id), /does not match/);
  assert.equal(f.calls.permission.length, 0);
  assert.equal(f.calls.query.length, 0);
});

test('permission denial and unexpected partition prevent cookie delivery', async () => {
  const denied = fixture({permissions: {request: async () => false, contains: async () => false}});
  const deniedGrant = await approve(denied);
  await assert.rejects(denied.exporter.exportFromUserGesture(deniedGrant), /permission was denied/);
  assert.equal(denied.calls.delivery.length, 0);
  assert.equal(denied.calls.query.length, 0);

  const partitioned = fixture({cookies: {
    getAllCookieStores: async () => [{id: 'firefox-container-2', tabIds: [7]}],
    getAll: async () => [{...partitioned.cookie, partitionKey: {topLevelSite: 'https://news.example'}}],
  }});
  const partitionedGrant = await approve(partitioned);
  await assert.rejects(partitioned.exporter.exportFromUserGesture(partitionedGrant), /selected partition/);
  assert.equal(partitioned.calls.delivery.length, 0);
  assert.equal(JSON.stringify(partitioned.calls.audit).includes(partitioned.cookie.value), false);
});

test('advanced scope requires an adapter capability assertion before approval', async () => {
  const f = fixture();
  await assert.rejects(f.exporter.prepare({...f.request, partition: {topLevelSite: 'https://news.example'}}),
    /has not confirmed partitioned/);
  await assert.rejects(f.exporter.prepare({...f.request, firstPartyDomain: 'news.example'}),
    /has not confirmed first-party/);
  assert.equal(f.calls.permission.length, 0);
});
