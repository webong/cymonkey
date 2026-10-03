import {existsSync, readFileSync, readdirSync, statSync} from 'node:fs';
import {homedir, platform as hostPlatform} from 'node:os';
import path from 'node:path';
import type {BrowserName} from '@steipete/sweet-cookie';

export type LocalCookieSource = {
  browser: BrowserName;
  label: string;
  profile?: string;
  safariCookiesFile?: string;
};

export type LocalCookieDiscoveryOptions = {
  homeDirectory?: string;
  platform?: NodeJS.Platform;
  appDataDirectory?: string;
  localAppDataDirectory?: string;
  /** Browser profile roots, or an exact profile directory. */
  additionalRoots?: Partial<Record<BrowserName, readonly string[]>>;
};

/** Discover local cookie stores without reading cookie values. The caller
 * chooses one source and authorizes a later query. */
export function discoverLocalCookieSources(options: LocalCookieDiscoveryOptions = {}): LocalCookieSource[] {
  const home = options.homeDirectory ?? homedir();
  const operatingSystem = options.platform ?? hostPlatform();
  const roots = defaultRoots(operatingSystem, home, options);
  const result: LocalCookieSource[] = [];
  const seen = new Set<string>();

  for (const browser of ['chrome', 'edge', 'firefox', 'safari'] as const) {
    for (const root of [...roots[browser], ...(options.additionalRoots?.[browser] ?? [])]) {
      const absoluteRoot = path.resolve(root);
      const candidates = browser === 'safari' ? [absoluteRoot]
        : hasCookieDatabase(browser, absoluteRoot) ? [absoluteRoot]
          : childDirectories(absoluteRoot);
      const displayNames = browser === 'chrome' || browser === 'edge' ? chromiumDisplayNames(absoluteRoot) : {};
      for (const candidate of candidates) {
        if (!hasCookieDatabase(browser, candidate)) continue;
        const key = `${browser}\0${candidate}`;
        if (seen.has(key)) continue;
        seen.add(key);
        const name = displayNames[path.basename(candidate)] ?? path.basename(candidate);
        result.push(browser === 'safari'
          ? {browser, label: `Safari (${path.basename(candidate)})`, safariCookiesFile: candidate}
          : {browser, label: `${browserLabel(browser)} — ${name}`, profile: candidate});
      }
    }
  }
  return result.sort((a, b) => a.browser.localeCompare(b.browser)
    || (a.profile ?? a.safariCookiesFile ?? '').localeCompare(b.profile ?? b.safariCookiesFile ?? ''));
}

function defaultRoots(
  operatingSystem: NodeJS.Platform,
  home: string,
  options: LocalCookieDiscoveryOptions,
): Record<BrowserName, string[]> {
  if (operatingSystem === 'darwin') {
    return {
      chrome: [path.join(home, 'Library/Application Support/Google/Chrome')],
      edge: [path.join(home, 'Library/Application Support/Microsoft Edge')],
      firefox: [path.join(home, 'Library/Application Support/Firefox/Profiles')],
      safari: [
        path.join(home, 'Library/Cookies/Cookies.binarycookies'),
        path.join(home, 'Library/Containers/com.apple.Safari/Data/Library/Cookies/Cookies.binarycookies'),
      ],
    };
  }
  if (operatingSystem === 'win32') {
    const local = options.localAppDataDirectory ?? process.env.LOCALAPPDATA ?? path.join(home, 'AppData/Local');
    const roaming = options.appDataDirectory ?? process.env.APPDATA ?? path.join(home, 'AppData/Roaming');
    return {
      chrome: [path.join(local, 'Google/Chrome/User Data')],
      edge: [path.join(local, 'Microsoft/Edge/User Data')],
      firefox: [path.join(roaming, 'Mozilla/Firefox/Profiles')],
      safari: [],
    };
  }
  return {
    chrome: [path.join(home, '.config/google-chrome'), path.join(home, '.config/chromium')],
    edge: [path.join(home, '.config/microsoft-edge')],
    firefox: [path.join(home, '.mozilla/firefox')],
    safari: [],
  };
}

function childDirectories(root: string): string[] {
  try {
    return readdirSync(root, {withFileTypes: true})
      .filter((entry) => entry.isDirectory())
      .map((entry) => path.join(root, entry.name));
  } catch { return []; }
}

function hasCookieDatabase(browser: BrowserName, source: string): boolean {
  if (browser === 'safari') return isFile(source);
  if (browser === 'firefox') return isFile(path.join(source, 'cookies.sqlite'));
  return isFile(path.join(source, 'Network/Cookies')) || isFile(path.join(source, 'Cookies'));
}

function isFile(candidate: string): boolean {
  try { return existsSync(candidate) && statSync(candidate).isFile(); }
  catch { return false; }
}

function chromiumDisplayNames(root: string): Record<string, string> {
  try {
    const parsed = JSON.parse(readFileSync(path.join(root, 'Local State'), 'utf8')) as {
      profile?: {info_cache?: Record<string, {name?: unknown}>};
    };
    return Object.fromEntries(Object.entries(parsed.profile?.info_cache ?? {})
      .filter((entry): entry is [string, {name: string}] => typeof entry[1]?.name === 'string')
      .map(([directory, info]) => [directory, info.name]));
  } catch { return {}; }
}

function browserLabel(browser: BrowserName): string {
  return browser === 'chrome' ? 'Chrome' : browser === 'edge' ? 'Edge' : 'Firefox';
}
