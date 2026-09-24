import assert from 'node:assert/strict';
import {spawn, execFileSync} from 'node:child_process';
import {mkdtemp, writeFile, access, rm} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import readline from 'node:readline';
import test from 'node:test';
import puppeteer from 'puppeteer-core';

const browserPath = process.env.CYMONKEY_CHROME_BIN ||
  (process.platform === 'darwin'
    ? '/Applications/Chrome.app/Contents/MacOS/Google Chrome'
    : '/usr/bin/chromium');

test('Jangolova reapplies extension-free script after navigation', {timeout: 45000}, async (t) => {
  try { await access(browserPath); } catch { t.skip('Chromium executable unavailable'); return; }
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'cymonkey-userscript-live-'));
  const html = path.join(temporary, 'page.html');
  await writeFile(html, "<!doctype html><html><head><meta http-equiv=\"Content-Security-Policy\" content=\"script-src 'self'\"></head><body>test</body></html>");
  const browser = await puppeteer.launch({
    executablePath: browserPath, headless: true,
    userDataDir: path.join(temporary, 'profile'),
    args: process.platform === 'linux' ? ['--no-sandbox'] : [],
  });
  const page = await browser.newPage();
  const worker = spawn(process.execPath, ['scripts/cymonkey-worker.mjs'], {
    cwd: process.cwd(), stdio: ['pipe', 'pipe', 'pipe'],
  });
  let stderr = '';
  worker.stderr.setEncoding('utf8').on('data', (chunk) => { stderr += chunk; });
  let nextID = 0;
  const pending = new Map();
  readline.createInterface({input: worker.stdout}).on('line', (line) => {
    const message = JSON.parse(line);
    const entry = pending.get(message.id);
    if (!entry) return;
    pending.delete(message.id);
    if (message.error) entry.reject(new Error(message.error));
    else entry.resolve(message.result);
  });
  const call = (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++nextID;
    pending.set(id, {resolve, reject});
    worker.stdin.write(JSON.stringify({id, method, params}) + '\n');
  });
  let disconnected = false;
  try {
    await call('connect', {
      endpoint: browser.wsEndpoint(), protocol: 'cdp', driver: 'puppeteer',
    });
    await call('act', {
      name: 'script.register', input: {
        augmentationId: 'cymonkey-managed-userscripts',
        script: {id: 'example', source: "globalThis.__cymonkeyInjected = 'yes';"},
        matches: ['file://*/*'], excludeMatches: [],
      },
    });
    const url = pathToFileURL(html).href;
    await page.goto(url);
    assert.equal(await page.evaluate(() => globalThis.__cymonkeyInjected), 'yes');
    await page.goto('about:blank');
    await page.goto(url);
    assert.equal(await page.evaluate(() => globalThis.__cymonkeyInjected), 'yes');
    const newPage = await browser.newPage();
    await newPage.goto(url);
    await newPage.waitForFunction(() => globalThis.__cymonkeyInjected === 'yes', {timeout: 3000});
    assert.equal(await newPage.evaluate(() => globalThis.__cymonkeyInjected), 'yes');
    await call('act', {
      name: 'script.unregister', input: {augmentationId: 'cymonkey-managed-userscripts', id: 'example'},
    });
    await page.goto('about:blank');
    await page.goto(url);
    assert.equal(await page.evaluate(() => globalThis.__cymonkeyInjected), undefined);
    await call('act', {
      name: 'script.register', input: {
        augmentationId: 'cymonkey-managed-userscripts',
        script: {id: 'example', source: "globalThis.__cymonkeyInjected = 'yes';"},
        matches: ['file://*/*'], excludeMatches: [],
      },
    });
    await page.goto('about:blank');
    await page.goto(url);
    assert.equal(await page.evaluate(() => globalThis.__cymonkeyInjected), 'yes');
    await call('disconnect');
    disconnected = true;
    await page.goto('about:blank');
    await page.goto(url);
    assert.equal(await page.evaluate(() => globalThis.__cymonkeyInjected), undefined);
  } finally {
    if (!disconnected) await call('disconnect').catch(() => {});
    worker.kill();
    await browser.close();
    await rm(temporary, {recursive: true, force: true});
  }
  assert.equal(stderr, '');
});

test('cmy stores and replays a userscript through Jangolova', {timeout: 45000}, async (t) => {
  const cmy = process.env.CYMONKEY_CMY_BIN;
  if (!cmy) { t.skip('set CYMONKEY_CMY_BIN to run the CLI integration test'); return; }
  try { await access(browserPath); } catch { t.skip('Chromium executable unavailable'); return; }
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'cymonkey-userscript-cli-'));
  const html = path.join(temporary, 'page.html');
  const script = path.join(temporary, 'script.js');
  const store = path.join(temporary, 'store');
  await writeFile(html, '<!doctype html><html><body>test</body></html>');
  await writeFile(script, "// ==UserScript==\n// @name Managed\n// @match file://*/*\n// ==/UserScript==\nglobalThis.__managedScript = 'active';");
  const browser = await puppeteer.launch({
    executablePath: browserPath, headless: true,
    userDataDir: path.join(temporary, 'profile'),
    args: process.platform === 'linux' ? ['--no-sandbox'] : [],
  });
  const page = await browser.newPage();
  let target;
  let provider;
  try {
    provider = spawn(cmy, [
      'userscript', 'install', '--store', store, '--source', script,
      '--endpoint', `cdp=${browser.wsEndpoint()}`,
    ], {cwd: process.cwd(), stdio: ['ignore', 'pipe', 'pipe']});
    let output = '';
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error(`provider did not connect: ${output}`)), 15000);
      provider.stdout.setEncoding('utf8').on('data', (chunk) => {
        output += chunk;
        const records = output.split('\n').flatMap((line) => { try { return [JSON.parse(line)]; } catch { return []; } });
        target = records.find((record) => record.status === 'stored')?.target;
        if (target && records.some((record) => record.status === 'connected')) {
          clearTimeout(timeout);
          resolve();
        }
      });
      provider.once('exit', (code) => { clearTimeout(timeout); reject(new Error(`provider exited ${code}: ${output}`)); });
    });
    await page.goto(pathToFileURL(html).href);
    assert.equal(await page.evaluate(() => globalThis.__managedScript), 'active');
    execFileSync(cmy, ['userscript', 'disable', '--store', store, '--target', target, '--id', 'managed']);
    let disabled = false;
    for (let attempt = 0; attempt < 20 && !disabled; attempt++) {
      await new Promise((resolve) => setTimeout(resolve, 250));
      await page.goto('about:blank');
      await page.goto(pathToFileURL(html).href);
      disabled = (await page.evaluate(() => globalThis.__managedScript)) === undefined;
    }
    assert.equal(disabled, true, 'provider did not unregister the disabled script');
  } finally {
    provider?.kill('SIGINT');
    await browser.close();
    await rm(temporary, {recursive: true, force: true});
  }
});
