import assert from 'node:assert/strict';
import { access, readFile } from 'node:fs/promises';

const root = new URL('../.output/', import.meta.url);
const browsers = ['chrome', 'edge', 'firefox', 'safari'];

for (const browser of browsers) {
  await verify(`${browser}-mv3`);
}

async function verify(directory) {
  const output = new URL(`${directory}/`, root);
  const manifest = JSON.parse(await readFile(new URL('manifest.json', output), 'utf8'));
  assert.equal(manifest.manifest_version, 3, `${directory}: expected MV3`);
  assert.equal(manifest.name, 'Jangolova Browser Extension');
  const supportsSandbox = directory === 'chrome-mv3' || directory === 'edge-mv3';
  if (supportsSandbox) {
    assert.deepEqual(manifest.sandbox?.pages, ['runtime-sandbox.html'], `${directory}: Jangolova sandbox missing`);
  } else {
    assert.ok(!manifest.sandbox, `${directory}: unsupported sandbox must be omitted`);
  }
  if (directory === 'safari-mv3') {
    assert.ok(!manifest.permissions.includes('management'), `${directory}: unsupported management permission must be omitted`);
    assert.ok(!manifest.permissions.includes('userScripts'), `${directory}: unsupported userscript permission must be omitted`);
    assert.ok(!manifest.externally_connectable, `${directory}: unsupported external connection entry point must be omitted`);
  } else {
    assert.ok(manifest.permissions.includes('management'), `${directory}: Xallet Spook discovery permission missing`);
    assert.ok(manifest.permissions.includes('userScripts'), `${directory}: native userscript permission missing`);
    assert.ok(manifest.externally_connectable, `${directory}: Xallet Spook control entry point missing`);
  }
  assert.ok(manifest.permissions.includes('scripting'));
  assert.ok(manifest.permissions.includes('declarativeNetRequest'));
  for (const path of ['background.js', 'control.html', 'popup.html', 'cymonkey-main.js', 'content-scripts/cymonkey.js']) {
    await access(new URL(path, output));
  }
}

console.log('verified Jangolova builds for Chrome, Edge, Firefox, and the capability-limited Safari container');
