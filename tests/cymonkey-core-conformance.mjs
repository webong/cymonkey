import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);
const source = async (path) => JSON.parse(await readFile(new URL(path, root), 'utf8'));

test('the portable core publishes the Cymonkey v1alpha1 contract', async () => {
  const [protocol, augmentation, scenePlan, userscript] = await Promise.all([
    source('protocol/cymonkey/v1alpha2/protocol.schema.json'),
    source('protocol/cymonkey/v1alpha2/augmentation.schema.json'),
    source('protocol/cymonkey/v1alpha2/scene-plan.schema.json'),
    source('protocol/cymonkey/userscript/v1alpha1/userscript.schema.json'),
  ]);

  assert.equal(protocol.$defs.hello.properties.protocolVersion.const, 'cymonkey/v1alpha1');
  assert.deepEqual(protocol.$defs.domain.enum, ['viewer', 'render', 'player']);
  assert.equal(protocol.$defs.driver.type, 'string');
  assert.match('contributor-driver', new RegExp(protocol.$defs.driver.pattern));
  assert.deepEqual(protocol.$defs.action.dependentRequired, { domain: ['runtime'], runtime: ['domain'] });

  assert.equal(augmentation.properties.apiVersion.const, 'cymonkey/v1alpha1');
  assert.deepEqual(augmentation.$defs.target.properties.domain.enum, ['viewer', 'render', 'player']);
  assert.equal(
    augmentation.$defs.viewerPayload.properties.userscripts.items.$ref,
    '../userscript/v1alpha1/userscript.schema.json',
  );
  assert.ok(augmentation.$defs.renderPayload.properties.scripts);
  assert.ok(augmentation.$defs.renderPayload.properties.styles);
  assert.equal(augmentation.$defs.viewerPayload.properties.scripts, undefined);
  assert.equal(augmentation.$defs.viewerPayload.properties.styles, undefined);
  assert.equal(scenePlan.properties.apiVersion.const, 'cymonkey.scene/v1alpha1');
  assert.equal(userscript.properties.apiVersion.const, 'cymonkey.userscript/v1alpha1');
});
