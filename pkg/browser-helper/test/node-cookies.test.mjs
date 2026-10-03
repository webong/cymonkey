import assert from 'node:assert/strict';
import {mkdtempSync, mkdirSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {DatabaseSync} from 'node:sqlite';
import test from 'node:test';
import {createLocalCookieReader, discoverLocalCookieSources, toCookieHeader} from '../dist/node.js';

test('host reader requires approval and reads only the selected Firefox profile and name', async () => {
  const root = mkdtempSync(path.join(tmpdir(), 'cymonkey-cookie-reader-'));
  const profile = path.join(root, 'profile');
  mkdirSync(profile);
  try {
    const database = new DatabaseSync(path.join(profile, 'cookies.sqlite'));
    database.exec(`
      CREATE TABLE moz_cookies (
        name TEXT, value TEXT, host TEXT, path TEXT, expiry INTEGER,
        isSecure INTEGER, isHttpOnly INTEGER, sameSite INTEGER
      );
      INSERT INTO moz_cookies VALUES
        ('session', 'selected', '.example.com', '/', 4102444800, 1, 1, 1),
        ('other', 'unselected', '.example.com', '/', 4102444800, 1, 1, 1),
        ('session', 'wrong-site', '.other.test', '/', 4102444800, 1, 1, 1);
    `);
    database.close();

    const request = {browser: 'firefox', profile, url: 'https://example.com/', names: ['session']};
    const denied = createLocalCookieReader({authorize: async () => false});
    await assert.rejects(denied.query(request), /denied/);

    const seen = [];
    const reader = createLocalCookieReader({authorize: async (scope) => { seen.push(scope); return true; }});
    const result = await reader.query(request);
    assert.equal(seen.length, 1);
    assert.equal(seen[0].profile, profile);
    assert.deepEqual(result.warnings, []);
    assert.equal(result.cookies.length, 1);
    assert.equal(result.cookies[0].value, 'selected');
    assert.equal(toCookieHeader(result.cookies), 'session=selected');
    const selected = await reader.querySource({browser: 'firefox', profile, label: 'Fixture'},
      {url: 'https://example.com/', names: ['session']});
    assert.equal(selected.cookies[0].value, 'selected');
    await assert.rejects(reader.query({...request, browser: 'safari'}), /profile selector/);
  } finally {
    rmSync(root, {recursive: true, force: true});
  }
});

test('metadata discovery finds browser profiles and still accepts an explicit root', () => {
  const home = mkdtempSync(path.join(tmpdir(), 'cymonkey-cookie-discovery-'));
  try {
    const chromeRoot = path.join(home, 'Library/Application Support/Google/Chrome');
    const chromeProfile = path.join(chromeRoot, 'Profile 1');
    mkdirSync(path.join(chromeProfile, 'Network'), {recursive: true});
    writeFileSync(path.join(chromeProfile, 'Network/Cookies'), '');
    writeFileSync(path.join(chromeRoot, 'Local State'), JSON.stringify({
      profile: {info_cache: {'Profile 1': {name: 'Work'}}},
    }));
    const firefoxProfile = path.join(home, 'custom-firefox-profile');
    mkdirSync(firefoxProfile);
    writeFileSync(path.join(firefoxProfile, 'cookies.sqlite'), '');
    const sources = discoverLocalCookieSources({
      platform: 'darwin', homeDirectory: home,
      additionalRoots: {firefox: [firefoxProfile]},
    });
    assert.deepEqual(sources.map(({browser, profile, label}) => ({browser, profile, label})), [
      {browser: 'chrome', profile: chromeProfile, label: 'Chrome — Work'},
      {browser: 'firefox', profile: firefoxProfile, label: 'Firefox — custom-firefox-profile'},
    ]);
  } finally {
    rmSync(home, {recursive: true, force: true});
  }
});
