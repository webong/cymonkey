import {getCookies, toCookieHeader, ALL_PROFILES} from '@steipete/sweet-cookie';
import type {BrowserName, Cookie, GetCookiesOptions} from '@steipete/sweet-cookie';
import {isAbsolute} from 'node:path';
import type {LocalCookieSource} from './node-profiles.js';

export {ALL_PROFILES, toCookieHeader};
export type {BrowserName, Cookie, CookieHeaderOptions, GetCookiesResult} from '@steipete/sweet-cookie';
export {discoverLocalCookieSources} from './node-profiles.js';
export type {LocalCookieSource, LocalCookieDiscoveryOptions} from './node-profiles.js';

export type LocalProfileSelection = string | readonly string[] | typeof ALL_PROFILES;

export type LocalCookieRequest = {
  browser: BrowserName;
  url: string;
  origins?: readonly string[];
  names?: readonly string[];
  profile?: LocalProfileSelection;
  chromiumBrowser?: GetCookiesOptions['chromiumBrowser'];
  safariCookiesFile?: string;
  includeExpired?: boolean;
  timeoutMs?: number;
};

export type LocalCookieResult = {
  browser: BrowserName;
  profile?: LocalCookieRequest['profile'];
  cookies: Cookie[];
  warnings: string[];
};

export type LocalCookieScope = Pick<LocalCookieRequest,
  'url' | 'origins' | 'names' | 'includeExpired' | 'timeoutMs'>;

export type LocalCookieReaderDependencies = {
  // The host resolves user approval and policy for the exact source and scope.
  authorize: (request: Readonly<LocalCookieRequest>) => boolean | Promise<boolean>;
};

/** Node-only local browser cookie reader. Import through
 * `@cymonkey/browser-helper/node`, never from an extension bundle. */
export function createLocalCookieReader(dependencies: LocalCookieReaderDependencies) {
  if (typeof dependencies?.authorize !== 'function') {
    throw new Error('local cookie access requires a caller authorization callback');
  }
  return {
    async querySource(source: LocalCookieSource, scope: LocalCookieScope): Promise<LocalCookieResult> {
      if (!source) throw new Error('select a browser cookie source');
      return this.query({
        browser: source.browser,
        ...(source.profile === undefined ? {} : {profile: source.profile}),
        ...(source.safariCookiesFile === undefined ? {} : {safariCookiesFile: source.safariCookiesFile}),
        ...scope,
      });
    },
    async query(input: LocalCookieRequest): Promise<LocalCookieResult> {
      const request = checkedRequest(input);
      if (!(await dependencies.authorize(request))) throw new Error('local cookie access was denied');
      const options: GetCookiesOptions = {
        url: request.url,
        browsers: [request.browser],
        mode: 'first',
        ...(request.origins === undefined ? {} : {origins: [...request.origins]}),
        ...(request.names === undefined ? {} : {names: [...request.names]}),
        ...(request.includeExpired === undefined ? {} : {includeExpired: request.includeExpired}),
        ...(request.timeoutMs === undefined ? {} : {timeoutMs: request.timeoutMs}),
        ...(request.chromiumBrowser === undefined ? {} : {chromiumBrowser: request.chromiumBrowser}),
        ...(request.safariCookiesFile === undefined ? {} : {safariCookiesFile: request.safariCookiesFile}),
      };
      if (request.profile !== undefined) {
        const profile = Array.isArray(request.profile) ? [...request.profile] : request.profile as GetCookiesOptions['profile'];
        if (request.browser === 'chrome') options.chromeProfile = profile;
        if (request.browser === 'edge') options.edgeProfile = profile;
        if (request.browser === 'firefox') options.firefoxProfile = profile;
      }
      const result = await getCookies(options);
      if (result.cookies.some((cookie) => cookie.source?.browser !== undefined && cookie.source.browser !== request.browser)) {
        throw new Error('cookie source did not match the selected browser');
      }
      return {browser: request.browser, profile: request.profile, cookies: result.cookies, warnings: result.warnings};
    },
  };
}

function checkedRequest(input: LocalCookieRequest): Readonly<LocalCookieRequest> {
  if (!input || !['chrome', 'edge', 'firefox', 'safari'].includes(input.browser)) {
    throw new Error('select a supported browser');
  }
  let url: URL;
  try { url = new URL(input.url); }
  catch { throw new Error('cookie query needs an absolute HTTP(S) URL'); }
  if (!['http:', 'https:'].includes(url.protocol) || !url.hostname) {
    throw new Error('cookie query needs an absolute HTTP(S) URL');
  }
  if (input.origins !== undefined && (!Array.isArray(input.origins) || input.origins.some((origin) => {
    try { const value = new URL(origin); return !['http:', 'https:'].includes(value.protocol) || !value.hostname; }
    catch { return true; }
  }))) throw new Error('cookie query origins must be absolute HTTP(S) URLs');
  if (input.names !== undefined && (!Array.isArray(input.names) || input.names.length === 0
    || input.names.some((name) => typeof name !== 'string' || !name))) {
    throw new Error('cookie names must be a nonempty list');
  }
  if (input.browser === 'safari' && input.profile !== undefined) {
    throw new Error('Safari uses a cookie-file selection, not a profile selector');
  }
  if (input.browser !== 'safari' && (input.profile === undefined
    || (typeof input.profile === 'string' && !input.profile.trim())
    || (Array.isArray(input.profile) && (input.profile.length === 0
      || input.profile.some((profile) => typeof profile !== 'string' || !profile.trim())))
    || (typeof input.profile !== 'string' && !Array.isArray(input.profile) && input.profile !== ALL_PROFILES))) {
    throw new Error('select a browser profile or explicitly select ALL_PROFILES');
  }
  if (input.browser !== 'safari' && input.safariCookiesFile !== undefined) {
    throw new Error('Safari cookie-file selection requires Safari');
  }
  if (input.safariCookiesFile !== undefined && !isAbsolute(input.safariCookiesFile)) {
    throw new Error('Safari cookie-file selection needs an absolute path');
  }
  if (input.browser !== 'chrome' && input.chromiumBrowser !== undefined) {
    throw new Error('Chromium browser selection requires the Chrome backend');
  }
  if (input.timeoutMs !== undefined && (!Number.isFinite(input.timeoutMs) || input.timeoutMs <= 0)) {
    throw new Error('cookie helper timeout must be positive');
  }
  return Object.freeze({...input,
    ...(input.names === undefined ? {} : {names: Object.freeze([...input.names])}),
    ...(input.origins === undefined ? {} : {origins: Object.freeze([...input.origins])}),
    ...(Array.isArray(input.profile) ? {profile: Object.freeze([...input.profile])} : {}),
  });
}
