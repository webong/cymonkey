import assert from 'node:assert/strict';
import {once} from 'node:events';
import {createServer} from 'node:http';
import {access, mkdir, mkdtemp, rm, stat} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {dirname, resolve} from 'node:path';
import test from 'node:test';
import puppeteer from 'puppeteer-core';

const root = resolve(new URL('..', import.meta.url).pathname);
const extensionPath = resolve(root, 'pkg/browser-ext/.output/chrome-mv3');
const evidencePath = resolve(root, 'output/playwright/threejs-browser-product-flow.png');
const chromePath = process.env.CYMONKEY_CHROME_BIN || '/Applications/Chrome.app/Contents/MacOS/Google Chrome';

test('reviewed Three.js package mounts in a live Chrome tab after popup approval', {timeout: 90_000}, async () => {
  await access(resolve(extensionPath, 'manifest.json'));
  await access(chromePath);

  const fixture = await startFixtureServer();
  const profile = await mkdtemp(resolve(tmpdir(), 'cymonkey-threejs-live-'));
  let browser;
  try {
    browser = await puppeteer.launch({
      executablePath: chromePath,
      // Chrome does not reliably start an unpacked MV3 service worker in
      // headless mode. This is intentionally a real, visible extension test.
      headless: false,
      ignoreDefaultArgs: ['--disable-extensions'],
      userDataDir: profile,
      args: [
        '--no-first-run',
        '--no-default-browser-check',
        '--use-angle=swiftshader',
        '--enable-unsafe-swiftshader',
        '--disable-gpu-sandbox',
      ],
    });

    const extensionId = await loadUnpackedExtension(browser);
    const target = await browser.newPage();
    await target.setViewport({width: 1280, height: 860, deviceScaleFactor: 1});
    await target.goto(fixture.url, {waitUntil: 'networkidle0'});
    assert.equal(await eventually(() => extensionID(browser)), extensionId);
    const popup = await browser.newPage();
    await popup.goto(`chrome-extension://${extensionId}/popup.html`, {waitUntil: 'domcontentloaded'});
    await popup.waitForFunction(() => document.querySelector('#status')?.textContent === 'Ready');
    await popup.waitForFunction(() => document.querySelector('#packages')?.textContent?.includes('Three.js Scene'));

    const reviewed = await extensionCall(popup, 'packages.list', {});
    assert.deepEqual(reviewed.map((item) => item.id), ['threejs']);
    assert.equal(reviewed[0].name, 'Three.js Scene');
    assert.deepEqual(reviewed[0].permissions, []);

    const tabId = await popup.evaluate(async (url) => {
      const tabs = await chrome.tabs.query({});
      const tab = tabs.find((candidate) => candidate.url === url);
      if (!tab?.id) throw new Error('live fixture tab was not available to the extension');
      return tab.id;
    }, fixture.url);

    const mountedInput = {
      augmentationId: 'live.threejs',
      id: 'live-threejs',
      package: 'threejs',
      permissions: [],
      target: {tabId},
      configuration: {},
      title: 'Three.js Scene',
    };
    const pending = await cymonkeyAct(popup, 'augmentation.mount', mountedInput);
    assert.equal(pending.status, 'approval-required');
    assert.ok(pending.approval?.id);

    await approveOrDenyInPopup(popup, 'Three.js Scene', 'approve');
    const mounted = await cymonkeyAct(popup, 'augmentation.mount', {...mountedInput, approvalId: pending.approval.id});
    assert.deepEqual(mounted, {
      ok: true,
      package: 'threejs',
      augmentationId: 'live.threejs',
      delivery: 'augmentation-package',
    });

    const overlay = await engineCall(popup, tabId, 'live.threejs', {
      id: 'overlay-1',
      method: 'act',
      params: {name: 'threejs.overlay.mount', input: {width: '420px', height: '280px', color: '#65d8ff'}},
    });
    assert.equal(overlay.id, 'overlay-1');
    assert.equal(overlay.result.ok, true);
    assert.equal(overlay.result.augmentationId, 'live.threejs');

    const added = await engineCall(popup, tabId, 'live.threejs', {
      id: 'object-1',
      method: 'act',
      params: {name: 'threejs.object.add', input: {id: 'proof-sphere', shape: 'sphere', color: '#ffd166'}},
    });
    assert.deepEqual(added.result, {
      ok: true,
      id: 'object:proof-sphere',
      materialId: 'material:proof-sphere',
      shape: 'sphere',
    });

    await target.waitForFunction((augmentationId) =>
      [...document.querySelectorAll('[data-jangolova-cymonkey-augmentation]')]
        .some((node) => node.getAttribute('data-jangolova-cymonkey-augmentation') === augmentationId),
    {}, 'live.threejs');
    await mkdir(dirname(evidencePath), {recursive: true});
    await target.screenshot({path: evidencePath, fullPage: true});
    assert.ok((await stat(evidencePath)).size > 4_000, 'the live overlay screenshot should contain rendered evidence');

    const events = await extensionCall(popup, 'cymonkey.call', {method: 'events', params: {limit: 256}});
    const eventTypes = new Set(events.events.map((event) => event.type));
    for (const expected of [
      'approval.package.requested', 'approval.package.approved', 'approval.package.consumed',
      'cymonkey.augmentation.mounted', 'cymonkey-engine.request.completed',
      'audit.control.requested', 'audit.control.succeeded',
    ]) assert.ok(eventTypes.has(expected), `missing live event ${expected}`);

    const unmounted = await cymonkeyAct(popup, 'augmentation.unmount', mountedInput);
    assert.deepEqual(unmounted, {ok: true, package: 'threejs', augmentationId: 'live.threejs'});
    await target.waitForFunction(() => !document.querySelector('[data-jangolova-cymonkey-augmentation]'));
    assert.equal(await target.$eval('#caller-owned', (node) => node.textContent), 'caller-owned page remains live');

    const deniedInput = {...mountedInput, augmentationId: 'live.threejs-denied', id: 'live-threejs-denied'};
    const denied = await cymonkeyAct(popup, 'augmentation.mount', deniedInput);
    assert.equal(denied.status, 'approval-required');
    await approveOrDenyInPopup(popup, 'Three.js Scene', 'deny');
    await assert.rejects(
      () => cymonkeyAct(popup, 'augmentation.mount', {...deniedInput, approvalId: denied.approval.id}),
      /package mount was denied by the user/,
    );
    const deniedEvents = await extensionCall(popup, 'cymonkey.call', {method: 'events', params: {limit: 256}});
    assert.ok(deniedEvents.events.some((event) => event.type === 'approval.package.denied'));
  } finally {
    await browser?.close();
    await fixture.close();
    await rm(profile, {recursive: true, force: true});
  }
});

async function cymonkeyAct(page, name, input) {
  return extensionCall(page, 'cymonkey.call', {method: 'act', params: {name, input}});
}

async function engineCall(page, tabId, augmentationId, request) {
  return extensionCall(page, 'cymonkey-engine.call', {
    augmentationId,
    delivery: 'augmentation-package',
    target: {tabId},
    request,
  });
}

async function extensionCall(page, method, params) {
  return page.evaluate(async ({method, params}) => chrome.runtime.sendMessage({
    channel: 'cymonkey.extension.control', method, params,
  }), {method, params});
}

async function approveOrDenyInPopup(page, packageName, decision) {
  await page.reload({waitUntil: 'domcontentloaded'});
  await page.waitForFunction((expected) => [...document.querySelectorAll('#approvals article')]
    .some((article) => article.textContent?.includes(expected)), {}, packageName);
  const text = await page.$eval('#approvals article', (article) => article.textContent || '');
  assert.match(text, /No additional browser permissions/);
  await page.evaluate(({expected, decision}) => {
    const article = [...document.querySelectorAll('#approvals article')]
      .find((candidate) => candidate.textContent?.includes(expected));
    const button = article?.querySelector(`button.${decision}`);
    if (!(button instanceof HTMLButtonElement)) throw new Error(`missing ${decision} button for ${expected}`);
    button.click();
  }, {expected: packageName, decision});
  const expectedStatus = decision === 'approve' ? 'Approved once' : 'Request denied.';
  await page.waitForFunction((expected) => document.querySelector('#status')?.textContent?.includes(expected), {}, expectedStatus);
}

async function extensionID(browser) {
  const worker = browser.targets().find((target) => target.type() === 'service_worker'
    && target.url().startsWith('chrome-extension://'));
  if (!worker) throw new Error(`Cymonkey extension service worker has not started; targets: ${browser.targets().map((target) => `${target.type()}:${target.url()}`).join(', ')}`);
  const match = /^chrome-extension:\/\/([^/]+)\//.exec(worker.url());
  if (!match) throw new Error('could not read Cymonkey extension ID');
  return match[1];
}

async function loadUnpackedExtension(browser) {
  const session = await browser.target().createCDPSession();
  const result = await session.send('Extensions.loadUnpacked', {path: extensionPath});
  if (typeof result.id !== 'string' || !result.id) throw new Error('Chrome did not return an unpacked extension ID');
  return result.id;
}

async function eventually(operation, timeoutMilliseconds = 15_000) {
  const expiresAt = Date.now() + timeoutMilliseconds;
  let lastError;
  while (Date.now() < expiresAt) {
    try { return await operation(); } catch (error) { lastError = error; }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw lastError || new Error('timed out waiting for browser state');
}

async function startFixtureServer() {
  const server = createServer((request, response) => {
    if (request.url !== '/') {
      response.writeHead(404).end();
      return;
    }
    response.writeHead(200, {'content-type': 'text/html; charset=utf-8'}).end(`<!doctype html>
      <title>Caller-owned live page</title>
      <main><h1>Existing site</h1><p id="caller-owned">caller-owned page remains live</p></main>`);
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('could not start local browser fixture');
  return {
    url: `http://127.0.0.1:${address.port}/`,
    close: () => new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve())),
  };
}
