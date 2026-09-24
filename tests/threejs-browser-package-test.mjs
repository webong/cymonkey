import assert from 'node:assert/strict';
import {readFile, stat} from 'node:fs/promises';
import test from 'node:test';
import vm from 'node:vm';

const root = new URL('../', import.meta.url);
const source = (path) => readFile(new URL(path, root), 'utf8');

test('Three.js is a reviewed ordinary augmentation package', async () => {
  const [manifestText, productText, packageSource, extensionPackage, extensionCapabilities] = await Promise.all([
    source('pkg/threejs-cymonkey/browser-package.json'),
    source('infra/browser/threejs.json'),
    source('pkg/threejs-cymonkey/src/content.ts'),
    source('pkg/browser-ext/package.json'),
    source('pkg/browser-ext/src/capabilities.ts'),
  ]);
  const manifest = JSON.parse(manifestText);
  const product = JSON.parse(productText);
  assert.deepEqual(manifest.spec.deliveries, [{
    kind: 'augmentation-package', entrypoint: 'content.js', browsers: ['chrome', 'edge', 'firefox', 'safari'],
  }]);
  assert.deepEqual(manifest.spec.permissions, []);
  assert.equal(manifest.spec.launch.name, 'threejs.overlay.mount');
  assert.deepEqual(product.spec.browsers, ['chrome', 'edge', 'firefox', 'safari']);
  assert.match(packageSource, /cymonkey\.jangolova\.browser-package\.factories/);
  assert.match(packageSource, /new WebGLRenderer/);
  assert.match(packageSource, /new ThreeJSCymonkey/);
  assert.doesNotMatch(extensionPackage, /threejs|"three"/i);
  assert.doesNotMatch(extensionCapabilities, /threejs\./);
});

test('every composed browser artifact carries the reviewed Three.js runtime', async () => {
  for (const browserName of ['chrome', 'edge', 'firefox', 'safari']) {
    const base = `pkg/browser-ext/.output/${browserName}-mv3`;
    const extensionManifest = JSON.parse(await source(`${base}/manifest.json`));
    assert.equal(extensionManifest.name, 'Cymonkey Three.js Browser Extension');
    assert.equal(extensionManifest.action.default_title, 'Cymonkey Three.js Browser Extension');
    const registry = JSON.parse(await source(`${base}/augmentations/registry.json`));
    assert.deepEqual(registry.packages, [{id: 'threejs', manifest: 'augmentations/threejs/manifest.json'}]);
    assert.ok((await stat(new URL(`${base}/augmentations/threejs/content.js`, root))).size > 100_000);
  }
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
