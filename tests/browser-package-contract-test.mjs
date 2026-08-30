import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);
const source = (path) => readFile(new URL(path, root), 'utf8');

test('reviewed package manifests constrain delivery, permissions, and CSP', async () => {
  const [manifestSchema, productSchema, packageManifest, product, builder] = await Promise.all([
    source('protocol/browser-package/v1alpha1/package.schema.json'),
    source('protocol/browser-package/v1alpha1/product.schema.json'),
    source('pkg/snapchat-camera-kit-cymonkey/browser-package.json'),
    source('products/browser/camera-kit.json'),
    source('scripts/build-browser-product.mjs'),
  ]);
  assert.equal(JSON.parse(manifestSchema).properties.kind.const, 'BrowserAugmentationPackage');
  assert.equal(JSON.parse(productSchema).properties.kind.const, 'BrowserExtensionProduct');
  assert.deepEqual(JSON.parse(packageManifest).spec.permissions, ['camera']);
  assert.deepEqual(JSON.parse(product).spec.browsers, ['chrome', 'edge']);
  assert.match(builder, /safeRepositoryPath/);
  assert.match(builder, /safeCSPSource/);
  assert.match(builder, /BrowserPackageRegistry/);
});

test('sensitive package mounts require one-time extension UI approval', async () => {
  const [approvals, engine, background, popup, policy] = await Promise.all([
    source('pkg/browser-ext/src/services/approvals.ts'),
    source('pkg/browser-ext/src/engine.ts'),
    source('pkg/browser-ext/entrypoints/background.ts'),
    source('pkg/browser-ext/entrypoints/popup/main.ts'),
    source('pkg/browser-ext/src/services/policy.ts'),
  ]);
  assert.match(engine, /status: 'approval-required'/);
  assert.match(approvals, /lifetimeMilliseconds = 5 \* 60 \* 1000/);
  assert.match(approvals, /approvals\.splice\(index, 1\)/);
  assert.doesNotMatch(approvals, /configuration|apiToken|token/);
  assert.match(background, /source !== 'extension-origin'/);
  assert.match(policy, /default-approval-ui/);
  assert.match(popup, /Allow once/);
  assert.match(popup, /approval\.resolve/);
});
