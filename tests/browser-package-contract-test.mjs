import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import {installEphemeralWebStorage} from '../pkg/browser-ext/src/sandbox-storage.js';

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
  assert.equal(JSON.parse(packageManifest).spec.launch.name, 'camera-kit.overlay.mount');
  assert.deepEqual(JSON.parse(product).spec.browsers, ['chrome', 'edge']);
  assert.match(builder, /safeRepositoryPath/);
  assert.match(builder, /safeCSPSource/);
  assert.match(builder, /BrowserPackageRegistry/);
});

test('sensitive package mounts require one-time extension UI approval', async () => {
  const [approvals, augmentations, background, popup, policy] = await Promise.all([
    source('pkg/browser-ext/src/services/approvals.ts'),
    source('pkg/browser-ext/src/services/augmentations.ts'),
    source('pkg/browser-ext/entrypoints/background.ts'),
    source('pkg/browser-ext/entrypoints/popup/main.ts'),
    source('pkg/browser-ext/src/services/policy.ts'),
  ]);
  assert.match(augmentations, /status: 'approval-required'/);
  assert.match(approvals, /lifetimeMilliseconds = 5 \* 60 \* 1000/);
  assert.match(approvals, /approvals\.splice\(index, 1\)/);
  assert.doesNotMatch(approvals, /input\.permissions\.length === 0/);
  assert.doesNotMatch(approvals, /configuration|apiToken|token/);
  assert.match(background, /source !== 'extension-origin'/);
  assert.match(policy, /default-approval-ui/);
  assert.match(policy, /default-standalone-package-ui/);
  assert.match(popup, /Allow once/);
  assert.match(popup, /approval\.resolve/);
  assert.match(popup, /packages\.list/);
  assert.match(popup, /augmentation\.mount/);
  assert.match(popup, /cymonkey-engine\.call/);
  assert.match(popup, /Configuration stays in this popup|configuration must be a JSON object/);
});

test('agent-authored userscripts receive revision-bound popup approval', async () => {
  const [runtime, service, managedRuntime, approvals, background, popup, docs] = await Promise.all([
    source('pkg/userscript-runtime/src/create.ts'),
    source('pkg/browser-ext/src/services/userscripts.ts'),
    source('pkg/browser-ext/src/services/userscript-runtime.ts'),
    source('pkg/browser-ext/src/services/userscript-approvals.ts'),
    source('pkg/browser-ext/entrypoints/background.ts'),
    source('pkg/browser-ext/entrypoints/popup/main.ts'),
    source('docs/userscripts.md'),
  ]);
  assert.match(runtime, /draft\.code\.includes\('==UserScript=='\)/);
  assert.match(runtime, /source: \{origin: 'user', code\}/);
  assert.match(service, /userscript\.prepare/);
  assert.match(service, /authorizeUserscriptMutation\('install'/);
  assert.match(service, /configureManagedWorld/);
  assert.doesNotMatch(service, /approvedPermissionIncrease|approveMainWorld|input\.approved/);
  assert.match(approvals, /userscript-approval-/);
  assert.match(approvals, /left\.revision === right\.revision/);
  assert.match(approvals, /publicDescription\(manifest\)/);
  assert.match(managedRuntime, /onUserScriptConnect/);
  assert.match(managedRuntime, /userscript action timed out/);
  assert.match(background, /listUserscriptApprovals/);
  assert.match(popup, /UserscriptApproval/);
  assert.match(docs, /userscript\.prepare/);
});

test('opaque sandbox storage is ephemeral and does not weaken the origin boundary', () => {
  const target = {};
  for (const name of ['localStorage', 'sessionStorage']) {
    Object.defineProperty(target, name, {
      configurable: true,
      get() { throw new DOMException('opaque origin', 'SecurityError'); },
    });
  }
  installEphemeralWebStorage(target);
  target.sessionStorage.setItem('camera-kit', 42);
  assert.equal(target.sessionStorage.getItem('camera-kit'), '42');
  assert.equal(target.sessionStorage.length, 1);
  assert.equal(target.localStorage.length, 0);
  assert.equal(Object.getPrototypeOf(target.sessionStorage), Object.prototype);
});
