import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);
const source = (path) => readFile(new URL(path, root), 'utf8');

test('privileged calls share fine-grained policy and redacted audit gates', async () => {
  const [policy, background, events] = await Promise.all([
    source('pkg/browser-ext/src/services/policy.ts'),
    source('pkg/browser-ext/entrypoints/background.ts'),
    source('pkg/browser-ext/src/services/events.ts'),
  ]);
  for (const dimension of ['callers', 'capabilities', 'effects', 'origins', 'tabIds', 'augmentationIds']) {
    assert.match(policy, new RegExp(dimension));
  }
  assert.match(policy, /matching\.find\(\(rule\) => rule\.decision === 'deny'\)/);
  assert.match(policy, /defaultDecision: 'deny'/);
  assert.match(policy, /policyMode: configured \? 'configured' : 'default-deny'/);
  assert.match(background, /policy\.authorize\(source, message\)/);
  for (const phase of ['requested', 'succeeded', 'denied', 'failed']) {
    assert.match(background, new RegExp(`publishAuditEvent\\('${phase}'`));
  }
  assert.match(events, /audit\.control\.\$\{phase\}/);
  const auditProjection = background.match(/function auditData\([\s\S]*?\n  \}/)?.[0] ?? '';
  assert.doesNotMatch(auditProjection, /params|token|request|source\.code/);
});

test('outbound WebSocket is optional, authenticated, bounded, and single-build', async () => {
  const [client, background, config, schema] = await Promise.all([
    source('pkg/browser-ext/src/outbound-control.ts'),
    source('pkg/browser-ext/entrypoints/background.ts'),
    source('pkg/browser-ext/wxt.config.ts'),
    source('src/protocol/browser-extension/v1alpha1/protocol.schema.json'),
  ]);
  assert.match(client, /CYMONKEY_EXTENSION_AUTH/);
  assert.match(client, /CYMONKEY_EXTENSION_AUTHENTICATED/);
  assert.match(client, /authenticated-websocket/);
  assert.match(client, /20_000/);
  assert.match(client, /30_000/);
  assert.match(client, /plaintext outbound control must use loopback/);
  assert.doesNotMatch(client.match(/describe\(\)[\s\S]*?\n  \}/)?.[0] ?? '', /token/);
  assert.match(background, /new OutboundControlClient/);
  assert.doesNotMatch(config, /mode.*websocket|websocket.*mode/);
  assert.match(schema, /control\.websocket\.configure/);
});
