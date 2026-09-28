import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {createServer} from 'node:http';
import {mkdtemp, readFile, rm, writeFile} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import puppeteer from 'puppeteer-core';

// An explicit opt-in fixture: launch only disposable test-owned browser profiles.
// Missing prerequisites fail rather than silently reporting a skipped success.
const cmy = process.env.CYMONKEY_CMY_BIN;
const executablePath = process.env.CYMONKEY_BROWSER_BIN;
const product = process.env.CYMONKEY_BROWSER_PRODUCT || 'chrome';

test('bookmarklet CLI exports executable, reversible links in a real browser', {timeout: 60000}, async () => {
  assert.ok(cmy && executablePath, 'set CYMONKEY_CMY_BIN and CYMONKEY_BROWSER_BIN');
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'jangolova-bookmarklet-'));
  const sourcePath = path.join(temporary, 'source.js');
  const urlPath = path.join(temporary, 'bookmark.txt');
  let browser;
  let html = '', csp = false;
  const server = createServer((_req, res) => {
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    if (csp) res.setHeader('Content-Security-Policy', "script-src 'none'");
    res.end(html);
  });
  try {
    const original = await readFile('examples/bookmarklets/highlight-links.js', 'utf8');
    const source = original + "\nwindow.bookmarkletText = '你好 + café %20 # &';\nreturn '<h1>must not replace the page</h1>'; // trailing comment";
    await writeFile(sourcePath, source);
    const exported = execFileSync(cmy, ['bookmarklet', 'export', '--source', sourcePath], {encoding: 'utf8'}).trim();
    await writeFile(urlPath, exported);
    assert.equal(execFileSync(cmy, ['bookmarklet', 'import', '--source', urlPath], {encoding: 'utf8'}), source);
    html = execFileSync(cmy, ['bookmarklet', 'export', '--source', sourcePath, '--format', 'html', '--name', '<script>bad()</script>'], {encoding: 'utf8'});
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    const address = `http://127.0.0.1:${server.address().port}`;
    browser = await puppeteer.launch({executablePath, browser: product, headless: true,
      userDataDir: path.join(temporary, 'profile'), args: process.platform === 'linux' && product === 'chrome' ? ['--no-sandbox'] : []});
    const page = await browser.newPage();
    await page.goto(address);
    assert.equal(await page.$('script'), null, 'HTML labels/source must never execute on load');
    assert.equal(await page.$('#jangolova-bookmarklet-highlight-links'), null);
    assert.equal(await page.$eval('#bookmarklet', (a) => a.getAttribute('href')), exported);
    const title = await page.title();
    await page.click('#bookmarklet');
    await page.waitForSelector('#jangolova-bookmarklet-highlight-links');
    assert.equal(await page.evaluate(() => window.bookmarkletText), '你好 + café %20 # &');
    assert.equal(await page.title(), title);
    assert.equal(page.url(), address + '/');
    await page.click('#bookmarklet');
    await page.waitForSelector('#jangolova-bookmarklet-highlight-links', {hidden: true});
    // A normal javascript: link respects CSP. This does not pretend to test
    // privileged bookmark-bar behavior, which differs between browsers.
    csp = true;
    await page.reload();
    await page.click('#bookmarklet');
    await new Promise((resolve) => setTimeout(resolve, 150));
    assert.equal(await page.$('#jangolova-bookmarklet-highlight-links'), null);
    assert.equal(await page.evaluate(() => window.bookmarkletText), undefined);
  } finally {
    await browser?.close();
    await new Promise((resolve) => server.close(resolve));
    await rm(temporary, {recursive: true, force: true});
  }
});
