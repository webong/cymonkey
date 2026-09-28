import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtemp, readFile, rm} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import puppeteer from 'puppeteer-core';

test('consuming web host approves, mounts, calls and cleans an augmentation', {timeout: 60000}, async () => {
  const executablePath = process.env.CYMONKEY_BROWSER_BIN;
  assert.ok(executablePath, 'set CYMONKEY_BROWSER_BIN');
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'cymonkey-augmentation-host-'));
  let browser;
  const server = createServer(async (req, res) => {
    try {
      if (/^\/modules\/(?:[a-z0-9-]+\/)*[a-z0-9-]+\.js$/.test(req.url)) {
        res.setHeader('Content-Type', 'text/javascript');
        res.end(await readFile(path.join('pkg/browser-adapter/dist', req.url.slice('/modules/'.length))));
      } else {
        res.setHeader('Content-Type', 'text/html');
        res.end('<!doctype html><title>Caller-owned augmentation host</title><p id="content">Original page</p>');
      }
    } catch { res.statusCode = 404; res.end(); }
  });
  try {
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const origin = `http://127.0.0.1:${server.address().port}`;
    const product = process.env.CYMONKEY_BROWSER_PRODUCT || 'chrome';
    browser = await puppeteer.launch({executablePath, browser: product, headless: true,
      userDataDir: path.join(temporary, 'profile'), args: product === 'chrome' && process.platform === 'linux' ? ['--no-sandbox'] : []});
    const page = await browser.newPage();
    await page.goto(origin);
    const result = await page.evaluate(async () => {
      const {createApprovalManager, createPackageCatalog, createAugmentationManager, createAugmentationRuntimeHost} = await import('/modules/index.js');
      const state = {}, events = [];
      let id = 0, injections = 0;
      const approvals = createApprovalManager({storage: {
        get: async (key) => ({[key]: structuredClone(state[key])}),
        set: async (items) => Object.assign(state, structuredClone(items)),
      }, newId: () => `approval-${++id}`});
      const runtime = createAugmentationRuntimeHost({factories: new Map([['reader', ({packageId, augmentationId}) => {
        const panel = document.createElement('aside');
        panel.id = augmentationId;
        panel.textContent = 'Mounted';
        document.body.append(panel);
        return {packageId, dispatch: (request) => { panel.textContent = request.text; return {text: panel.textContent}; }, unmount: () => panel.remove()};
      }]]), onEvent: (event) => events.push(event.type)});
      const manifest = {apiVersion: 'cymonkey.browser-package/v1alpha1', kind: 'BrowserAugmentationPackage',
        metadata: {id: 'reader', name: 'Reader', version: '1.0.0'}, spec: {deliveries: [{kind: 'augmentation-package', entrypoint: 'content.js', browsers: ['chrome', 'firefox']}], permissions: [], capabilities: ['reader.mount']}};
      const catalog = createPackageCatalog({browser: 'chrome', load: async (path) => path.endsWith('registry.json')
        ? {apiVersion: manifest.apiVersion, kind: 'BrowserPackageRegistry', packages: [{id: 'reader', manifest: 'augmentations/reader/manifest.json'}]}
        : manifest});
      // The web host supplies routing and preloaded assets. This fixture uses
      // real DOM but does not simulate privileged WebExtension API coverage.
      const manager = createAugmentationManager({catalog, approvals, tabs: {
        resolve: async () => ({id: 1, url: location.href}),
        send: async (_target, _channel, method, input) => method === 'mount'
          ? runtime.mount({packageId: input.package, augmentationId: input.augmentationId})
          : runtime.unmount(input.augmentationId, input.package),
      }, browser: {scripts: {execute: async () => { injections++; }}}});
      const input = {package: 'reader', augmentationId: 'reader-overlay', target: {tabId: 1}};
      const pending = await manager.mount(input);
      const beforeApproval = {injections, mounted: runtime.list().length};
      await approvals.resolve(pending.approval.id, 'deny');
      let denied = false;
      try { await manager.mount({...input, approvalId: pending.approval.id}); } catch { denied = true; }
      const next = await manager.mount(input);
      await approvals.resolve(next.approval.id, 'approve');
      let wrongScope = false;
      try { await manager.mount({...input, augmentationId: 'other', approvalId: next.approval.id}); } catch { wrongScope = true; }
      await manager.mount({...input, approvalId: next.approval.id});
      const changed = await runtime.call(input.augmentationId, {text: 'Updated by Jangolova'});
      let replayDenied = false;
      try { await manager.mount({...input, approvalId: next.approval.id}); } catch { replayDenied = true; }
      await manager.unmount(input);
      const removed = document.getElementById(input.augmentationId) === null;
      const again = await manager.mount(input);
      await approvals.resolve(again.approval.id, 'approve');
      await manager.mount({...input, approvalId: again.approval.id});
      const remounted = document.getElementById(input.augmentationId)?.textContent;
      await manager.unmount(input);
      return {beforeApproval, denied, wrongScope, changed, replayDenied, removed, remounted, events, active: runtime.list(), content: document.getElementById('content').textContent};
    });
    assert.deepEqual(result.beforeApproval, {injections: 0, mounted: 0});
    for (const key of ['denied', 'wrongScope', 'replayDenied', 'removed']) assert.equal(result[key], true, key);
    assert.deepEqual(result.changed, {text: 'Updated by Jangolova'});
    assert.equal(result.remounted, 'Mounted');
    assert.deepEqual(result.active, []);
    assert.equal(result.content, 'Original page');
    assert.deepEqual(result.events, ['augmentation.mounted', 'augmentation.unmounted', 'augmentation.mounted', 'augmentation.unmounted']);
    await page.reload();
    assert.equal(await page.$('#reader-overlay'), null);
  } finally {
    await browser?.close();
    await new Promise((resolve) => server.close(resolve));
    await rm(temporary, {recursive: true, force: true});
  }
});
