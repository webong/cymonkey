import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {mkdtemp, mkdir, writeFile, rm} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import readline from 'node:readline';
import {pathToFileURL} from 'node:url';
import test from 'node:test';

function startWorker() {
  const child = spawn(process.execPath, ['scripts/cymonkey-worker.mjs'], {stdio: ['pipe', 'pipe', 'pipe']});
  let sequence = 0, errors = '';
  const pending = new Map();
  child.stderr.setEncoding('utf8').on('data', (text) => { errors += text; });
  const lines = readline.createInterface({input: child.stdout});
  lines.on('line', (line) => {
    const message = JSON.parse(line), entry = pending.get(message.id);
    if (!entry) return;
    pending.delete(message.id);
    clearTimeout(entry.timer);
    if (message.error) entry.reject(new Error(message.error));
    else entry.resolve(message.result);
  });
  child.on('exit', () => {
    for (const entry of pending.values()) { clearTimeout(entry.timer); entry.reject(new Error(`worker exited: ${errors}`)); }
    pending.clear();
  });
  const call = (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++sequence;
    const timer = setTimeout(() => { pending.delete(id); reject(new Error(`${method} timed out: ${errors}`)); }, 10000);
    pending.set(id, {resolve, reject, timer});
    child.stdin.write(JSON.stringify({id, method, params}) + '\n');
  });
  return {child, call};
}

async function stop(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, 'exit');
  child.kill('SIGTERM');
  const timer = setTimeout(() => child.kill('SIGKILL'), 3000);
  try { await exited; } finally { clearTimeout(timer); }
}

test('Firefox BiDi userscripts navigate, unregister and detach without taking the target', {timeout: 60000}, async () => {
  assert.ok(process.env.CYMONKEY_FIREFOX_BIN, 'set CYMONKEY_FIREFOX_BIN');
  const temp = await mkdtemp(path.join(os.tmpdir(), 'jangolova-bidi-userscript-'));
  let firefox, worker;
  try {
    const profile = path.join(temp, 'profile');
    await mkdir(profile);
    const file = path.join(temp, 'page.html');
    await writeFile(file, '<!doctype html><title>Caller-owned Firefox</title><body>Fixture</body>');
    firefox = spawn(process.env.CYMONKEY_FIREFOX_BIN, ['--headless', '--no-remote', '--profile', profile, '--remote-debugging-port', '0', 'about:blank'], {stdio: ['ignore', 'pipe', 'pipe']});
    const endpoint = await new Promise((resolve, reject) => {
      let output = '';
      const timer = setTimeout(() => reject(new Error(`Firefox endpoint timed out: ${output}`)), 15000);
      const listen = (chunk) => {
        output += chunk;
        const match = output.match(/WebDriver BiDi listening on (ws:\/\/[^\s]+)/);
        if (match) { clearTimeout(timer); resolve(match[1].replace(/\/$/, '') + '/session'); }
      };
      firefox.stdout.on('data', listen);
      firefox.stderr.on('data', listen);
      firefox.once('error', (error) => { clearTimeout(timer); reject(error); });
      firefox.once('exit', () => { clearTimeout(timer); reject(new Error(`Firefox exited: ${output}`)); });
    });
    worker = startWorker();
    const connection = {endpoint, protocol: 'webdriver-bidi', driver: 'puppeteer'};
    await worker.call('connect', connection).catch((error) => { throw new Error(`initial attachment: ${error.message}`); });
    const act = (name, input) => worker.call('act', {name, input});
    const navigate = (url) => act('window.navigate', {url});
    const value = async () => (await act('window.evaluate', {expression: "globalThis.__managed || 'absent'"})).value;
    const url = pathToFileURL(file).href;
    const registration = {augmentationId: 'managed', script: {id: 'example', source: "globalThis.__managed = 'present';"}, matches: ['file://*/*'], excludeMatches: []};
    await act('script.register', registration);
    await navigate(url);
    assert.equal(await value(), 'present');
    await navigate('about:blank');
    await navigate(url);
    assert.equal(await value(), 'present');
    await act('script.unregister', {augmentationId: 'managed', id: 'example'});
    await navigate('about:blank');
    await navigate(url);
    assert.equal(await value(), 'absent');
    await act('script.register', registration);
    const exited = once(worker.child, 'exit');
    await worker.call('disconnect');
    await exited;
    assert.equal(firefox.exitCode, null);
    assert.equal(firefox.signalCode, null);
    worker = startWorker();
    await worker.call('connect', connection).catch((error) => { throw new Error(`attachment after disconnect: ${error.message}`); });
    assert.equal((await act('window.evaluate', {expression: 'document.title'})).value, 'Caller-owned Firefox');
    await navigate('about:blank');
    await navigate(url);
    assert.equal(await value(), 'absent', 'disconnect must remove registered preloads');
  } finally {
    await stop(worker?.child);
    await stop(firefox);
    await rm(temp, {recursive: true, force: true});
  }
});
