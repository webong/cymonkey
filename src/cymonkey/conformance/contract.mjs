import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../../../', import.meta.url);
const source = async (path) => JSON.parse(await readFile(new URL(path, root), 'utf8'));

test('the portable core publishes the Jangolova Cymonkey v1alpha2 contract', async () => {
  const [protocol, augmentation, scenePlan, userscript] = await Promise.all([
    source('src/cymonkey/protocol/v1alpha2/protocol.schema.json'),
    source('src/cymonkey/protocol/v1alpha2/augmentation.schema.json'),
    source('src/cymonkey/protocol/v1alpha2/scene-plan.schema.json'),
    source('src/cymonkey/protocol/userscript/v1alpha1/userscript.schema.json'),
  ]);

  assert.equal(protocol.$defs.hello.properties.protocolVersion.const, 'jangolova.cymonkey/v1alpha2');
  assert.deepEqual(protocol.$defs.domain.enum, ['computer', 'render', 'player']);
  assert.equal(protocol.$defs.driver.type, 'string');
  assert.match('contributor-driver', new RegExp(protocol.$defs.driver.pattern));
  assert.deepEqual(protocol.$defs.action.dependentRequired, { domain: ['runtime'], runtime: ['domain'] });

  assert.equal(augmentation.properties.apiVersion.const, 'jangolova.cymonkey/v1alpha2');
  assert.deepEqual(augmentation.$defs.target.properties.domain.enum, ['computer', 'render', 'player']);
  assert.equal(
    augmentation.$defs.computerPayload.properties.userscripts.items.$ref,
    '../userscript/v1alpha1/userscript.schema.json',
  );
  assert.equal(scenePlan.properties.apiVersion.const, 'jangolova.cymonkey.scene/v1alpha1');
  assert.equal(userscript.properties.apiVersion.const, 'jangolova.cymonkey.userscript/v1alpha1');
});
