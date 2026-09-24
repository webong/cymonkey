import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import vm from 'node:vm';

const root = new URL('../', import.meta.url);
const source = (path) => readFile(new URL(path, root), 'utf8');

test('Three.js is a reusable augmentation package', async () => {
  const [manifestText, packageSource] = await Promise.all([
    source('pkg/threejs-cymonkey/browser-package.json'),
    source('pkg/threejs-cymonkey/src/content.ts'),
  ]);
  const manifest = JSON.parse(manifestText);
  assert.deepEqual(manifest.spec.deliveries, [{
    kind: 'augmentation-package', entrypoint: 'content.js', browsers: ['chrome', 'edge', 'firefox', 'safari'],
  }]);
  assert.deepEqual(manifest.spec.permissions, []);
  assert.equal(manifest.spec.launch.name, 'threejs.overlay.mount');
  assert.match(packageSource, /cymonkey\.jangolova\.browser-package\.factories/);
  assert.match(packageSource, /new WebGLRenderer/);
  assert.match(packageSource, /new ThreeJSCymonkey/);
});

test('the bundled content entrypoint registers a usable private factory', async () => {
  const bundle = await source('pkg/threejs-cymonkey/dist/content.js');
  const context = vm.createContext({});
  vm.runInContext(bundle, context);
  const factory = vm.runInContext("globalThis[Symbol.for('cymonkey.jangolova.browser-package.factories')].get('threejs')", context);
  assert.equal(typeof factory, 'function');
  const runtime = factory({packageId: 'threejs', augmentationId: 'test.threejs', configuration: {}});
  const hello = await runtime.dispatch({id: 'hello-1', method: 'hello'});
  assert.equal(hello.id, 'hello-1');
  assert.equal(hello.result.protocolVersion, 'cymonkey/v1alpha1');
  assert.deepEqual(Array.from(hello.result.runtimes), ['threejs']);
});
