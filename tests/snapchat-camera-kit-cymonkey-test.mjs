import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);
const source = (path) => readFile(new URL(path, root), 'utf8');

test('Camera Kit capability lifetime conforms to the shared protocol', async () => {
  const schema = JSON.parse(await source('src/protocol/cymonkey/v1alpha2/protocol.schema.json'));
  const implementation = await source('pkg/snapchat-camera-kit-cymonkey/dist/index.js');
  const lifetime = implementation.match(/lifetime: ['"]([^'"]+)['"]/)?.[1];
  assert.ok(schema.$defs.capability.properties.lifetime.enum.includes(lifetime), `unsupported capability lifetime: ${lifetime}`);
});

test('Camera Kit declares a target-owned Cymonkey runtime with explicit user consent', async () => {
  const [declaration, implementation] = await Promise.all([
    source('pkg/snapchat-camera-kit-cymonkey/dist/index.d.ts'),
    source('pkg/snapchat-camera-kit-cymonkey/dist/index.js'),
  ]);
  assert.match(declaration, /class CameraKitCymonkey/);
  assert.match(implementation, /CAMERA_KIT_RUNTIME = 'snapchat-camera-kit'/);
  assert.match(implementation, /CAMERA_KIT_DRIVER = 'sandbox'/);
  for (const capability of [
    'camera-kit.overlay.mount', 'camera-kit.session.describe', 'camera-kit.lens.apply',
    'camera-kit.lens.remove', 'camera-kit.camera.stop', 'camera-kit.overlay.unmount',
  ]) assert.match(implementation, new RegExp(capability.replaceAll('.', '\\.')));
  assert.match(implementation, /getUserMedia/);
  assert.match(implementation, /Start camera/);
  assert.match(implementation, /installGlobal/);
});

test('Camera Kit remains a caller-owned library and browser package', async () => {
  const [packageReadme, packageSource, sandboxSource, tsconfig] = await Promise.all([
    source('pkg/snapchat-camera-kit-cymonkey/README.md'),
    source('pkg/snapchat-camera-kit-cymonkey/src/index.ts'),
    source('pkg/snapchat-camera-kit-cymonkey/src/sandbox.ts'),
    source('pkg/snapchat-camera-kit-cymonkey/tsconfig.json'),
  ]);
  assert.match(packageReadme, /consuming extension can bundle this package/);
  assert.match(packageSource, /Start camera/);
  assert.match(packageSource, /getUserMedia/);
  assert.match(packageSource, /mediaProvider/);
  assert.match(sandboxSource, /cymonkey\.jangolova\.media\.request/);
  assert.match(sandboxSource, /RTCPeerConnection/);
  assert.match(packageSource, /installGlobal/);
  assert.deepEqual(JSON.parse(tsconfig).include, ['src/index.ts', 'src/sandbox.ts']);
});
