import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createManifest,
  parseMetadata,
  permissionIncrease,
  registrationID,
  registrationPlan,
  requiresRenewedApproval,
  sourceRevision,
  validateManifest,
} from '../dist/index.js';

const source = `// ==UserScript==
// @name Example Enhancer
// @namespace https://jangolova.dev/examples
// @version 1.0.0
// @match https://example.com/*
// @grant none
// @run-at document-idle
// ==/UserScript==
document.documentElement.dataset.enhanced = 'true';`;

async function manifest(overrides = {}) {
  return {
    apiVersion: 'cymonkey.userscript/v1alpha1', kind: 'UserScript',
    metadata: {id: 'example-enhancer', revision: await sourceRevision(source), name: 'Example Enhancer'},
    spec: {
      matches: ['https://example.com/*'], excludeMatches: [], runAt: 'document_idle',
      world: 'USER_SCRIPT', allFrames: false, grants: ['none'], enabled: false,
    },
    source: {origin: 'user', code: source},
    ...overrides,
  };
}

test('parses and validates a bounded grant-none userscript', async () => {
  assert.equal(parseMetadata(source).name, 'Example Enhancer');
  const value = validateManifest(await manifest());
  const plan = registrationPlan(value);
  assert.equal(plan.id, 'jg-us-example-enhancer');
  assert.equal(plan.js[0].code, source);
  assert.equal(plan.world, 'USER_SCRIPT');
  assert.notEqual(registrationID('example.enhancer'), registrationID('example-enhancer'));
});

test('rejects undeclared grants and unapproved main-world source', async () => {
  const grants = await manifest();
  grants.spec.grants = ['GM_xmlhttpRequest'];
  await assert.rejects(async () => validateManifest(grants), /grant none/);
  const main = await manifest();
  main.spec.world = 'MAIN';
  assert.throws(() => validateManifest(main), /explicit approval/);
});

test('detects permission increases before update', async () => {
  const previous = await manifest();
  const next = await manifest();
  next.spec.matches.push('https://other.example/*');
  const increase = permissionIncrease(previous, next);
  assert.deepEqual(increase.addedMatches, ['https://other.example/*']);
  assert.equal(requiresRenewedApproval(increase), true);
});

test('prepares a reviewable manifest from an agent-authored body', async () => {
  const value = await createManifest({
    id: 'agent-banner', name: 'Agent Banner', matches: ['https://example.com/*'],
    description: 'Adds a small page marker.', code: "document.documentElement.dataset.agentBanner = 'true';",
  });
  assert.equal(value.source.origin, 'user');
  assert.equal(value.spec.grants[0], 'none');
  assert.match(value.source.code, /@name Agent Banner/);
  assert.match(value.source.code, /dataset\.agentBanner/);
  assert.match(value.source.code, /'cymonkey'/);
  assert.match(value.source.code, /'jangolova'/);
  assert.match(value.source.code, /cymonkey\.userscript\/v1alpha1/);
  await assert.rejects(() => createManifest({
    id: 'bad-header', name: 'Bad Header', matches: ['https://example.com/*'], code: '// ==UserScript==',
  }), /metadata block/);
});
