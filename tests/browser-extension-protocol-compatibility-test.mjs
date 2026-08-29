import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = new URL('../', import.meta.url);
test('generated browser-extension bindings match the checked-in schema', async () => {
  const result = spawnSync(process.execPath, ['scripts/generate-browser-extension-protocol.mjs', '--check'], {
    cwd: new URL('.', root), encoding: 'utf8',
  });
  assert.equal(result.status, 0, result.stderr || result.stdout);
  const [schema, typescript, go] = await Promise.all([
    readFile(new URL('protocol/browser-extension/v1alpha1/protocol.schema.json', root), 'utf8'),
    readFile(new URL('pkg/browser-ext/src/generated/browser-extension-v1alpha1.ts', root), 'utf8'),
    readFile(new URL('internal/browserextensionprotocol/generated_v1alpha1.go', root), 'utf8'),
  ]);
  const parsed = JSON.parse(schema);
  const methods = parsed.$defs.controlCall.allOf[0].then.properties.method.enum;
  for (const method of new Set(methods)) {
    assert.match(typescript, new RegExp(JSON.stringify(method).replaceAll('.', '\\.')));
    assert.match(go, new RegExp(JSON.stringify(method).replaceAll('.', '\\.')));
  }
});

test('browser control protocol accepts one Jangolova envelope', async () => {
  const schema = JSON.parse(await readFile(new URL('protocol/browser-extension/v1alpha1/protocol.schema.json', root), 'utf8'));
  assert.equal(schema.$defs.controlCall.properties.type.const, 'JANGOLOVA_EXTENSION_CALL');
});
