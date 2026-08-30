import assert from 'node:assert/strict';
import {access, readFile, stat} from 'node:fs/promises';
import test from 'node:test';

const root = new URL('../', import.meta.url);
const source = (path) => readFile(new URL(path, root), 'utf8');

test('Camera Kit is composed only into Chrome-family sandbox builds', async () => {
  const extensionPackage = await source('pkg/browser-ext/package.json');
  assert.doesNotMatch(extensionPackage, /camera-kit|@snap\//);
  for (const target of ['chrome-mv3', 'edge-mv3']) {
    const manifest = JSON.parse(await source(`pkg/browser-ext/.output/${target}/manifest.json`));
    assert.deepEqual(manifest.sandbox?.pages, ['runtime-sandbox.html']);
    assert.match(manifest.content_security_policy?.sandbox || '', /cf-st\.sc-cdn\.net/);
    assert.match(manifest.content_security_policy?.sandbox || '', /snapar\.com/);
    const asset = new URL(`pkg/browser-ext/.output/${target}/augmentations/snapchat-camera-kit/sandbox.js`, root);
    await access(asset);
    assert.ok((await stat(asset)).size >= 100_000);
  }
});
