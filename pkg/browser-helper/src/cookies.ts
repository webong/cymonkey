import type {createApprovalManager} from './approvals.js';
import type {createControlGate} from './control-gate.js';

// This API runs in a privileged extension page or background context. An
// ordinary page or content script cannot supply the WebExtension cookies API.
export type NativeCookie = {
  name: string;
  value: string;
  domain: string;
  path: string;
  secure: boolean;
  httpOnly: boolean;
  hostOnly: boolean;
  session: boolean;
  sameSite: string;
  storeId: string;
  expirationDate?: number;
  firstPartyDomain?: string;
  partitionKey?: {topLevelSite?: string; hasCrossSiteAncestor?: boolean; [key: string]: unknown};
  [key: string]: unknown;
};

export type NativeCookieAPI = {
  getAll(details: Record<string, unknown>): Promise<NativeCookie[]>;
  getAllCookieStores(): Promise<Array<{id: string; tabIds?: number[]}>>;
};

export type NativeCookiePermissions = {
  request(details: {permissions: string[]; origins: string[]}): Promise<boolean>;
  contains(details: {permissions: string[]; origins: string[]}): Promise<boolean>;
};

export type CookieSelection = {
  browser: string;
  profile: string;
  storeId: string;
  tabId?: number;
};

export type CookieExportRequest = {
  selection: CookieSelection;
  sites: string[];
  names: string[];
  partition?: 'unpartitioned' | {topLevelSite: string};
  firstPartyDomain?: string;
};

export type CookieExportPayload = {
  format: 'cymonkey.webextension-cookies/v1';
  source: 'browser-api';
  selection: CookieSelection;
  sites: string[];
  names: string[];
  partition: 'unpartitioned' | {topLevelSite: string};
  firstPartyDomain?: string;
  cookies: NativeCookie[];
};

export type CookieExportGrant = Readonly<{id: string}>;

export type CookieExporterDependencies = {
  cookies?: NativeCookieAPI;
  permissions?: NativeCookiePermissions;
  approvals: ReturnType<typeof createApprovalManager>;
  gate: ReturnType<typeof createControlGate>;
  caller: string;
  // Assert these only when the integrating extension knows its browser's
  // cookies.getAll implementation honors the corresponding filter.
  scopeSupport?: {partitionKey?: boolean; firstPartyDomain?: boolean};
  // Resolve this from trusted extension/host state, never from a page message.
  selected: () => Promise<CookieSelection>;
  // Deliver only over an authenticated host channel or an explicit user export.
  // The library does not log, persist, or transmit cookie values itself.
  deliver: (payload: CookieExportPayload) => Promise<void>;
};

type NormalizedRequest = Omit<CookieExportRequest, 'partition'> & {partition: 'unpartitioned' | {topLevelSite: string}};
type Prepared = {request: NormalizedRequest; permission: {permissions: string[]; origins: string[]}};

export function createCookieExporter(dependencies: CookieExporterDependencies) {
  if (!dependencies?.approvals?.request || !dependencies.gate?.authorize || !dependencies.gate?.run
    || typeof dependencies.selected !== 'function' || typeof dependencies.deliver !== 'function'
    || typeof dependencies.caller !== 'string' || !dependencies.caller) {
    throw new Error('cookie export needs approval, policy, a trusted selection, and a delivery callback');
  }
  const grants = new WeakMap<CookieExportGrant, Prepared>();
  let sequence = 0;

  function requireBrowserAPI() {
    if (typeof dependencies.cookies?.getAll !== 'function' || typeof dependencies.cookies.getAllCookieStores !== 'function'
      || typeof dependencies.permissions?.request !== 'function' || typeof dependencies.permissions.contains !== 'function') {
      throw new Error('cookie export requires a privileged extension context with cookies and permissions APIs');
    }
    return {cookies: dependencies.cookies, permissions: dependencies.permissions};
  }

  function contexts(request: NormalizedRequest) {
    return request.sites.map((site) => ({caller: dependencies.caller, capability: 'cookies.export', effect: 'external' as const,
      origin: new URL(site).origin}));
  }

  return {
    availability() {
      try { requireBrowserAPI(); return {available: true as const}; }
      catch { return {available: false as const, reason: 'A privileged extension context with cookies and permissions APIs is required'}; }
    },

    // Prepare may be asynchronous. The integrating UI displays the selected
    // scope and obtains its ordinary single-use approval before the next click.
    async prepare(input: CookieExportRequest, approvalId?: string) {
      requireBrowserAPI();
      const request = normalizeRequest(input);
      if (request.partition !== 'unpartitioned' && dependencies.scopeSupport?.partitionKey !== true) {
        throw new Error('this browser adapter has not confirmed partitioned cookie queries');
      }
      if (request.firstPartyDomain !== undefined && dependencies.scopeSupport?.firstPartyDomain !== true) {
        throw new Error('this browser adapter has not confirmed first-party domain queries');
      }
      if (!sameSelection(await dependencies.selected(), request.selection)) {
        throw new Error('selected browser, profile, or cookie store changed');
      }
      for (const context of contexts(request)) {
        if (dependencies.gate.authorize(context).decision !== 'allow') throw new Error('control policy denied cookie export');
      }
      const encoded = new TextEncoder().encode(JSON.stringify(request));
      const hash = await globalThis.crypto?.subtle?.digest('SHA-256', encoded);
      if (!hash) throw new Error('cookie export approval requires Web Crypto');
      const revision = 'sha256:' + Array.from(new Uint8Array(hash), (byte) => byte.toString(16).padStart(2, '0')).join('');
      const permission = {permissions: ['cookies'], origins: [...new Set(request.sites.map(hostPermission))]};
      const approval = await dependencies.approvals.request({
        kind: 'cookie', subjectId: 'browser-cookies', revision, targetId: revision,
        permissions: [...permission.permissions, ...permission.origins],
      }, approvalId);
      if (!approval.approved) return {status: 'approval-required' as const, approval: approval.approval, request};
      const grant = Object.freeze({id: `cookie-export-${++sequence}`});
      grants.set(grant, {request, permission});
      return {status: 'ready' as const, grant, request};
    },

    // Call directly from the approved Export button's click handler. The
    // permissions.request call occurs before any await, preserving the gesture.
    async exportFromUserGesture(grant: CookieExportGrant) {
      const prepared = grants.get(grant);
      if (!prepared) throw new Error('cookie export approval is missing or already used');
      grants.delete(grant);
      const {cookies, permissions} = requireBrowserAPI();
      const {request, permission} = prepared;
      for (const context of contexts(request)) {
        if (dependencies.gate.authorize(context).decision !== 'allow') throw new Error('control policy denied cookie export');
      }
      // Do not move an await or an asynchronous approval check above this call.
      let requested: Promise<boolean>;
      try { requested = Promise.resolve(permissions.request(permission)).catch(() => false); }
      catch { throw new Error('browser cookie or host permission request failed'); }
      const operation = async () => {
        let allowed: boolean;
        try { allowed = await requested && await permissions.contains(permission); }
        catch { throw new Error('browser cookie or host permission request failed'); }
        if (!allowed) throw new Error('browser cookie or host permission was denied');
        if (!sameSelection(await dependencies.selected(), request.selection)) {
          throw new Error('selected browser, profile, or cookie store changed');
        }
        let stores: Array<{id: string; tabIds?: number[]}>;
        try { stores = await cookies.getAllCookieStores(); }
        catch { throw new Error('browser cookie stores are unavailable'); }
        const store = stores.find((item) => item.id === request.selection.storeId);
        if (!store || (request.selection.tabId !== undefined && !store.tabIds?.includes(request.selection.tabId))) {
          throw new Error('selected cookie store is no longer available for the selected tab');
        }
        const found = new Map<string, NativeCookie>();
        for (const site of request.sites) {
          for (const name of request.names) {
            const details: Record<string, unknown> = {url: site, name, storeId: request.selection.storeId};
            if (request.partition !== 'unpartitioned') details.partitionKey = request.partition;
            if (request.firstPartyDomain !== undefined) details.firstPartyDomain = request.firstPartyDomain;
            let rows: NativeCookie[];
            try { rows = await cookies.getAll(details); }
            catch { throw new Error('browser rejected the selected cookie scope or permissions'); }
            if (!Array.isArray(rows)) throw new Error('browser returned an invalid cookie result');
            for (const row of rows) {
              const cookie = checkedCookie(row, request, site, name);
              const key = JSON.stringify([cookie.storeId, cookie.firstPartyDomain ?? '', cookie.partitionKey ?? null,
                cookie.name, cookie.domain, cookie.path]);
              const previous = found.get(key);
              if (previous && JSON.stringify(previous) !== JSON.stringify(cookie)) {
                throw new Error('a cookie changed during export; retry the request');
              }
              found.set(key, cookie);
            }
          }
        }
        if (!sameSelection(await dependencies.selected(), request.selection)) {
          throw new Error('selected browser, profile, or cookie store changed');
        }
        const payload: CookieExportPayload = {format: 'cymonkey.webextension-cookies/v1', source: 'browser-api',
          selection: request.selection, sites: request.sites, names: request.names, partition: request.partition,
          ...(request.firstPartyDomain === undefined ? {} : {firstPartyDomain: request.firstPartyDomain}),
          cookies: [...found.values()]};
        let size: number;
        try { size = new TextEncoder().encode(JSON.stringify(payload)).byteLength; }
        catch { throw new Error('browser returned a cookie that cannot be exported'); }
        if (size > (8 << 20)) {
          throw new Error('cookie export exceeds the 8 MiB input limit');
        }
        try { await dependencies.deliver(payload); }
        catch { throw new Error('cookie export delivery failed'); }
        return {status: 'delivered' as const, count: payload.cookies.length, selection: request.selection};
      };
      const auditContexts = contexts(request);
      const run = (index: number): Promise<Awaited<ReturnType<typeof operation>>> => index === auditContexts.length
        ? operation() : dependencies.gate.run(auditContexts[index]!, () => run(index + 1));
      return run(0);
    },
  };
}

function normalizeRequest(input: CookieExportRequest): NormalizedRequest {
  if (!input || !input.selection || !Array.isArray(input.sites) || input.sites.length < 1 || input.sites.length > 16
    || !Array.isArray(input.names) || input.names.length < 1 || input.names.length > 32) {
    throw new Error('cookie export requires a selected browser, 1–16 sites, and 1–32 names');
  }
  const selection = input.selection;
  if (!/^[a-z][a-z0-9_-]{0,63}$/.test(selection.browser) || !selection.profile.trim() || selection.profile.length > 512
    || /[\x00-\x1f\x7f]/.test(selection.profile) || !selection.storeId || selection.storeId.length > 256
    || /[\x00-\x1f\x7f]/.test(selection.storeId)
    || (selection.tabId !== undefined && (!Number.isInteger(selection.tabId) || selection.tabId < 0))) {
    throw new Error('cookie export browser, profile, store, or tab selection is invalid');
  }
  const sites = input.sites.map((site) => normalizeSite(site));
  const names = input.names.map((name) => {
    if (typeof name !== 'string' || name.length < 1 || name.length > 256 || /[\x00-\x1f\x7f]/.test(name)) {
      throw new Error('cookie export name is invalid');
    }
    return name;
  });
  let partition: NormalizedRequest['partition'] = 'unpartitioned';
  if (input.partition !== undefined && input.partition !== 'unpartitioned') {
    if (!input.partition || typeof input.partition.topLevelSite !== 'string') throw new Error('cookie partition selection is invalid');
    const topLevelSite = normalizeSite(input.partition.topLevelSite);
    if (topLevelSite !== new URL(topLevelSite).origin + '/') throw new Error('cookie partition needs a top-level site origin');
    partition = {topLevelSite: new URL(topLevelSite).origin};
  }
  if (input.firstPartyDomain !== undefined && (typeof input.firstPartyDomain !== 'string' || input.firstPartyDomain.length > 256
    || /[\x00-\x20\x7f/]/.test(input.firstPartyDomain))) {
    throw new Error('cookie first-party domain selection is invalid');
  }
  return {
    selection: {browser: selection.browser, profile: selection.profile, storeId: selection.storeId,
      ...(selection.tabId === undefined ? {} : {tabId: selection.tabId})},
    sites: [...new Set(sites)].sort(), names: [...new Set(names)].sort(), partition,
    ...(input.firstPartyDomain === undefined ? {} : {firstPartyDomain: input.firstPartyDomain}),
  };
}

function normalizeSite(value: string) {
  if (typeof value !== 'string' || value.length > 2048) throw new Error('cookie export site is invalid');
  let site: URL;
  try { site = new URL(value); }
  catch { throw new Error('cookie export site is invalid'); }
  if (!['http:', 'https:'].includes(site.protocol) || site.username || site.password || !site.hostname) {
    throw new Error('cookie export site must be an HTTP(S) URL without credentials');
  }
  site.hash = '';
  site.search = '';
  return site.toString();
}

function hostPermission(site: string) {
  const url = new URL(site);
  return `${url.protocol}//${url.hostname}/*`;
}

function sameSelection(actual: CookieSelection, expected: CookieSelection) {
  return Boolean(actual && actual.browser === expected.browser && actual.profile === expected.profile
    && actual.storeId === expected.storeId && actual.tabId === expected.tabId);
}

function checkedCookie(raw: NativeCookie, request: NormalizedRequest, site: string, name: string): NativeCookie {
  if (!raw || typeof raw !== 'object' || typeof raw.name !== 'string' || raw.name !== name
    || typeof raw.value !== 'string' || typeof raw.domain !== 'string' || typeof raw.path !== 'string'
    || typeof raw.storeId !== 'string' || raw.storeId !== request.selection.storeId
    || typeof raw.secure !== 'boolean' || typeof raw.httpOnly !== 'boolean' || typeof raw.hostOnly !== 'boolean'
    || typeof raw.session !== 'boolean' || typeof raw.sameSite !== 'string'
    || (!raw.session && (!Number.isFinite(raw.expirationDate) || raw.expirationDate === undefined || raw.expirationDate < 0))
    || (raw.session && raw.expirationDate !== undefined)
    || (raw.firstPartyDomain !== undefined && typeof raw.firstPartyDomain !== 'string')
    || (raw.partitionKey !== undefined && (typeof raw.partitionKey !== 'object' || raw.partitionKey === null
      || (raw.partitionKey.topLevelSite !== undefined && typeof raw.partitionKey.topLevelSite !== 'string')
      || (raw.partitionKey.hasCrossSiteAncestor !== undefined && typeof raw.partitionKey.hasCrossSiteAncestor !== 'boolean')))) {
    throw new Error('browser returned an incomplete or out-of-scope cookie');
  }
  const url = new URL(site);
  const domain = raw.domain.replace(/^\./, '').toLowerCase();
  const host = url.hostname.toLowerCase();
  const path = url.pathname || '/';
  if (!domain || (raw.hostOnly && raw.domain.startsWith('.'))
    || !(raw.hostOnly ? host === domain : host === domain || host.endsWith('.' + domain))
    || !raw.path.startsWith('/') || !(path === raw.path || raw.path.endsWith('/') && path.startsWith(raw.path)
      || path.startsWith(raw.path + '/')) || (raw.secure && url.protocol !== 'https:')) {
    throw new Error('browser returned a cookie outside the requested site');
  }
  if (request.firstPartyDomain === undefined ? Boolean(raw.firstPartyDomain) : raw.firstPartyDomain !== request.firstPartyDomain) {
    throw new Error('browser returned a cookie outside the selected first-party domain');
  }
  if (request.partition === 'unpartitioned' ? Boolean(raw.partitionKey?.topLevelSite)
    : raw.partitionKey?.topLevelSite !== request.partition.topLevelSite) {
    throw new Error('browser returned a cookie outside the selected partition');
  }
  try { return structuredClone(raw); }
  catch { throw new Error('browser returned a cookie that cannot be exported'); }
}
