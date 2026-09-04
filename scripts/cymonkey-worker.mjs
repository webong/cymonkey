#!/usr/bin/env node
import process from "node:process";
import readline from "node:readline";

const protocolVersion = "cymonkey/v1alpha1";
let browser;
let targetProtocol = "cdp";
let disconnected = true;
let extensionControl = null;
let extensionControlCreated = false;
let extensionConfig = { mode: "auto", id: "" };
let policy = { allowedCapabilities: [], allowedOrigins: [] };
let negotiated = [];
let sequence = 0;
const events = [];
const augmentations = new Map();
const registrations = new Map();
const styles = new Map();
const storage = new Map();
const observedPages = new WeakSet();
const networkRules = new Map();
const interceptionHandlers = new Map();

const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
let chain = Promise.resolve();
lines.on("line", (line) => {
  chain = chain.then(() => handleLine(line)).catch((error) => console.error(error?.stack || String(error)));
});

async function handleLine(line) {
  let request;
  try {
    request = JSON.parse(line);
    const result = await dispatch(request.method, request.params || {});
    respond({ id: request.id, result });
    if (request.method === "disconnect") setImmediate(() => process.exit(0));
  } catch (error) {
    respond({ id: request?.id || 0, error: error?.message || String(error) });
  }
}

async function dispatch(method, params) {
  if (method === "connect") return connect(params);
  if (method === "reconnect") return reconnect(params);
  if (method === "disconnect") return disconnect();
  if (method === "health") return { connected: isConnected(), drivers: activeDrivers() };
  requireConnection();
  if (method === "hello") return hello();
  if (method === "capabilities") return negotiated;
  if (method === "describe") return describe();
  if (method === "act") return act(params);
  if (method === "events") return readEvents(params);
  throw new Error(`unsupported Cymonkey method ${method}`);
}

let driver = "auto";

async function connect(params) {
  if (typeof params.endpoint !== "string" || !params.endpoint) throw new Error("caller-owned browser endpoint is required");
  targetProtocol = params.protocol === "webdriver-bidi" ? "webdriver-bidi" : "cdp";
  driver = typeof params.driver === "string" && params.driver ? params.driver : "auto";
  extensionConfig = normalizeExtension(params.extension);
  policy = normalizePolicy(params.policy);
  browser = await openBrowser(params.endpoint, targetProtocol, params.headers, driver);
  disconnected = false;
  observeBrowser(browser);
  await initializePages();
  await probeExtension();
  negotiated = await negotiateCapabilities();
  appendEvent("cymonkey.connected", { drivers: activeDrivers(), driver });
  return { capabilities: negotiated.map((item) => item.name), descriptors: negotiated, drivers: activeDrivers() };
}

async function reconnect(params) {
  requireConnection();
  const previous = browser;
  const previousControl = extensionControl;
  const previousCreated = extensionControlCreated;
  targetProtocol = params.protocol === "webdriver-bidi" ? "webdriver-bidi" : "cdp";
  driver = typeof params.driver === "string" && params.driver ? params.driver : driver;
  extensionConfig = normalizeExtension(params.extension);
  policy = normalizePolicy(params.policy);
  const replacement = await openBrowser(params.endpoint, targetProtocol, params.headers, driver);
  browser = replacement;
  disconnected = false;
  extensionControl = null;
  extensionControlCreated = false;
  observeBrowser(replacement);
  try {
    await initializePages();
    await probeExtension();
    negotiated = await negotiateCapabilities();
  } catch (error) {
    await disableInterception(await getPages().catch(() => []));
    browser = previous;
    extensionControl = previousControl;
    extensionControlCreated = previousCreated;
    replacement.disconnect();
    throw error;
  }
  await disableInterception(await getPages().catch(() => []));
  if (previousCreated && previousControl && !previousControl.isClosed()) await previousControl.close().catch(() => {});
  if (typeof previous.disconnect === "function") previous.disconnect();
  else if (typeof previous.close === "function") await previous.close().catch(() => {});
  appendEvent("cymonkey.connection.renewed", { drivers: activeDrivers(), driver });
  return { reconnected: true, drivers: activeDrivers() };
}

async function openBrowser(endpoint, protocol, headers, driver = "auto") {
  if (driver === "playwright") {
    if (protocol !== "cdp") throw new Error("Playwright driver requires CDP protocol");
    const { chromium } = await import("playwright-core");
    return chromium.connectOverCDP(endpoint, { headers: safeHeaders(headers) });
  }
  const { default: puppeteer } = await import("puppeteer-core");
  const connectionEndpoint = protocol === "cdp" ? await resolveCDPEndpoint(endpoint, headers) : endpoint;
  return puppeteer.connect({
    browserWSEndpoint: connectionEndpoint,
    protocol: protocol === "webdriver-bidi" ? "webDriverBiDi" : "cdp",
    headers: safeHeaders(headers),
  });
}

function observeBrowser(candidate) {
  candidate.on("disconnected", () => {
    if (browser !== candidate) return;
    disconnected = true;
    appendEvent("cymonkey.disconnected", {});
  });
  candidate.on("targetcreated", async (target) => {
    const page = await target.page().catch(() => null);
    if (page) await initializePage(page).catch(() => {});
  });
}

async function getPages() {
  if (!browser) return [];
  if (typeof browser.pages === "function") return browser.pages();
  if (typeof browser.contexts === "function") return browser.contexts().flatMap((context) => context.pages());
  return [];
}

// Puppeteer exposes Page.addScriptToEvaluateOnNewDocument directly. Playwright
// deliberately keeps that API behind a CDP session, so use the protocol there
// rather than degrading registered scripts into a non-removable context script.
async function addPreloadScript(page, fn, ...args) {
  if (typeof page.evaluateOnNewDocument === "function") {
    const handle = await page.evaluateOnNewDocument(fn, ...args);
    return { kind: "puppeteer", identifier: handle.identifier };
  }
  if (typeof page.context === "function") {
    const session = await page.context().newCDPSession(page);
    try {
      const result = await session.send("Page.addScriptToEvaluateOnNewDocument", {
        source: `(${fn.toString()})(${args.map((value) => JSON.stringify(value)).join(",")});`,
      });
      return { kind: "cdp", identifier: result.identifier };
    } finally {
      await session.detach().catch(() => {});
    }
  }
  throw new Error("browser page does not support preload scripts");
}

async function removePreloadScript(page, handle) {
  if (handle?.kind === "puppeteer" && typeof page.removeScriptToEvaluateOnNewDocument === "function") {
    await page.removeScriptToEvaluateOnNewDocument(handle.identifier);
    return;
  }
  if (handle?.kind === "cdp" && typeof page.context === "function") {
    const session = await page.context().newCDPSession(page);
    try {
      await session.send("Page.removeScriptToEvaluateOnNewDocument", { identifier: handle.identifier });
    } finally {
      await session.detach().catch(() => {});
    }
    return;
  }
  throw new Error("browser page does not support removing preload scripts");
}

async function initializePages() {
  for (const page of await getPages()) await initializePage(page);
}

async function initializePage(page) {
  if (observedPages.has(page) || isExtensionPage(page)) return;
  observedPages.add(page);
  const semanticBackend = targetProtocol === "webdriver-bidi" ? "bidi" : "cdp";
  await addPreloadScript(page, pageBridgeBootstrap, semanticBackend);
  await page.evaluate(pageBridgeBootstrap, semanticBackend).catch(() => {});
  for (const registration of registrations.values()) {
    const handle = await addPreloadScript(page, scriptBootstrap, registration.source, registration.matches, registration.excludeMatches);
    registration.handles.push({ page, handle });
  }
  page.on("request", (request) => {
    if (!capabilityAllowed("network.observe")) return;
    appendEvent("network.request", { url: redactURL(request.url()), method: request.method(), resourceType: request.resourceType() });
  });
  if (networkRules.size > 0) await enableInterception(page);
}

async function probeExtension() {
  if (extensionConfig.mode === "disabled") return;
  if (targetProtocol !== "cdp" || !extensionConfig.id) {
    if (extensionConfig.mode === "required") throw new Error("required Cymonkey extension needs a CDP backend and provider-supplied extension ID");
    return;
  }
  try {
    const result = await openExtensionControl(browser, extensionConfig.id);
    extensionControl = result.page;
    extensionControlCreated = result.created;
    const extensionHello = await callExtension("hello", {});
    if (extensionHello?.protocolVersion !== protocolVersion ||
      !extensionHello?.domains?.includes("viewer") ||
      !extensionHello?.runtimes?.includes("browser-dom") ||
      !extensionHello?.drivers?.includes("webextension") ||
      extensionHello?.implementation?.name !== "cymonkey-browser-extension-webextension") {
      throw new Error("extension returned an incompatible Cymonkey handshake");
    }
  } catch (error) {
    extensionControl = null;
    if (extensionConfig.mode === "required") throw error;
    appendEvent("webextension.unavailable", { reason: error?.message || String(error) });
  }
}

async function openExtensionControl(candidate, extensionId) {
  if (!/^[a-p]{32}$/.test(extensionId)) throw new Error("CDP WebExtension probing requires a 32-character Chrome extension ID");
  const url = `chrome-extension://${extensionId}/control.html`;
  const existing = (await getPages()).find((page) => page.url() === url);
  const page = existing || await candidate.newPage();
  try {
    if (!existing) await page.goto(url, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => document.documentElement.dataset.cymonkeyControlReady === "true" && typeof globalThis.cymonkeyDispatch === "function", { timeout: 5000 });
    return { page, created: !existing };
  } catch (error) {
    if (!existing && !page.isClosed()) await page.close().catch(() => {});
    throw error;
  }
}

async function negotiateCapabilities() {
  const base = (await probeBaseCapabilities()).filter((item) => capabilityAllowed(item.name));
  if (!extensionControl) return base;
  const extension = (await callExtension("capabilities", {})).map(normalizeExtensionCapability).filter((item) => capabilityAllowed(item.name));
  const merged = new Map(base.map((item) => [item.name, item]));
  for (const item of extension) {
    const fallback = merged.get(item.name);
    if (fallback) item.alternatives = [...new Set([...(item.alternatives || []), fallback.driver])];
    merged.set(item.name, item);
  }
  return [...merged.values()].sort((left, right) => left.name.localeCompare(right.name));
}

function hello() {
  return {
    protocolVersion,
    implementation: { name: "cymonkey", version: "0.1.0" },
    domains: ["viewer", "render"],
    runtimes: ["browser-dom"],
    drivers: activeDrivers(),
    features: ["augmentation", "caller-owned-target", "capabilities.negotiated", "events.cursor", "page-bridge.nested"],
  };
}

async function describe() {
  const pages = (await getPages()).filter((page) => !isExtensionPage(page));
  return {
    revision: String(sequence),
    surfaces: await Promise.all(pages.map(async (page, index) => ({
      id: `document:${index}`, domain: "render", runtime: "browser-dom", kind: "document",
      label: await page.title(), properties: { url: page.url() },
    }))),
    augmentations: [...augmentations.values()].map((item) => ({ id: item.id, revision: item.revision, enabled: item.enabled, domains: ["render"] })).sort((a, b) => a.id.localeCompare(b.id)),
  };
}

async function act(request) {
  const name = String(request?.name || "");
  const input = request?.input && typeof request.input === "object" ? request.input : {};
  const descriptor = negotiated.find((item) => item.name === name);
  if (!descriptor) throw new Error(`Cymonkey capability ${JSON.stringify(name)} is unavailable or denied by policy`);
  await enforceOriginPolicy(input);
  let result;
  if (descriptor.driver === "webextension") result = await callExtension("act", { name, input });
  else result = await actBase(name, input);
  appendEvent("cymonkey.action", { name, driver: descriptor.driver });
  return result;
}

async function actBase(name, input) {
  if (name === "augmentation.install") return installAugmentation(input.manifest, false);
  if (name === "augmentation.update") return installAugmentation(input.manifest, true);
  if (name === "augmentation.uninstall") return uninstallAugmentation(requireString(input.augmentationId, "augmentationId"));
  if (name === "augmentation.enable") return setAugmentationEnabled(requireString(input.augmentationId, "augmentationId"), true);
  if (name === "augmentation.disable") return setAugmentationEnabled(requireString(input.augmentationId, "augmentationId"), false);
  if (name === "augmentation.list") return { augmentations: [...augmentations.values()].map(publicAugmentation) };
  if (name === "augmentation.describe") return publicAugmentation(requireAugmentation(input.augmentationId));
  if (name === "window.navigate") {
    const page = await targetPage(input);
    requireString(input.url, "url");
    const response = await page.goto(input.url, { waitUntil: "domcontentloaded" });
    return { url: page.url(), status: response?.status?.() ?? null };
  }
  if (name === "window.click") {
    const page = await targetPage(input);
    requireString(input.selector, "selector");
    if (typeof page.locator === "function") {
      await page.locator(input.selector).click();
    } else {
      await page.click(input.selector);
    }
    return { clicked: true };
  }
  if (name === "window.fill") {
    const page = await targetPage(input);
    requireString(input.selector, "selector");
    requireString(input.value, "value");
    if (typeof page.locator === "function" && typeof page.locator(input.selector).fill === "function") {
      await page.locator(input.selector).fill(input.value);
    } else if (typeof page.type === "function") {
      await page.click(input.selector, { clickCount: 3 }).catch(() => {});
      await page.type(input.selector, input.value);
    } else {
      await page.click(input.selector).catch(() => {});
      await page.evaluate(({ selector, value }) => {
        const el = document.querySelector(selector);
        if (el) { el.value = value; el.dispatchEvent(new Event("input", { bubbles: true })); }
      }, { selector: input.selector, value: input.value });
    }
    return { filled: true };
  }
  if (name === "window.press") {
    const page = await targetPage(input);
    requireString(input.selector, "selector");
    requireString(input.key, "key");
    if (typeof page.locator === "function") {
      await page.locator(input.selector).press(input.key);
    } else {
      await page.focus(input.selector);
      await page.keyboard.press(input.key);
    }
    return { pressed: input.key };
  }
  if (name === "window.evaluate") {
    const page = await targetPage(input);
    requireString(input.expression, "expression");
    return { value: await page.evaluate(input.expression) };
  }
  if (name === "window.screenshot") {
    const page = await targetPage(input);
    return { pngBase64: await page.screenshot({ encoding: "base64", fullPage: Boolean(input.fullPage) }) };
  }
  if (name === "script.execute") return executeScript(input);
  if (name === "script.register") return registerScript(input);
  if (name === "script.unregister") return unregisterScript(input);
  if (name === "style.insert") return insertStyle(input);
  if (name === "style.remove") return removeStyle(input);
  if (name === "document.query") return domQuery(input);
  if (name === "document.observe") return domObserve(input);
  if (name === "document.patch") return domPatch(input);
  if (name === "overlay.mount") return overlayChange(input, "mount");
  if (name === "overlay.patch") return overlayChange(input, "patch");
  if (name === "overlay.unmount") return overlayChange(input, "unmount");
  if (name === "storage.get") return storageGet(input);
  if (name === "storage.set") return storageSet(input);
  if (name === "network.rules.install") return installNetworkRules(input);
  if (name === "network.rules.remove") return removeNetworkRules(input);
  throw new Error(`Cymonkey backend ${targetProtocol} does not implement ${name}`);
}

async function installAugmentation(manifest, update) {
  validateManifest(manifest);
  const target = browserTarget(manifest);
  const matches = target.match.urlPatterns;
  const excludeMatches = target.match.excludeUrlPatterns || [];
  for (const match of [...matches, ...excludeMatches]) {
    if (!originPatternAllowed(match)) throw new Error(`Cymonkey policy denied augmentation match ${JSON.stringify(match)}`);
  }
  const id = manifest.metadata.id;
  if (update !== augmentations.has(id)) throw new Error(`augmentation ${JSON.stringify(id)} ${update ? "is not installed" : "already exists"}`);
  if (update) await deactivateAugmentation(id);
  const record = { id, revision: manifest.metadata.revision, enabled: manifest.spec.enabled !== false, manifest, matches, excludeMatches };
  augmentations.set(id, record);
  if (record.enabled) await activateAugmentation(record);
  return publicAugmentation(record);
}

async function uninstallAugmentation(id) {
  requireAugmentation(id);
  await deactivateAugmentation(id);
  augmentations.delete(id);
  return { ok: true, augmentationId: id };
}

async function setAugmentationEnabled(id, enabled) {
  const record = requireAugmentation(id);
  if (record.enabled === enabled) return publicAugmentation(record);
  if (enabled) await activateAugmentation(record); else await deactivateAugmentation(id);
  record.enabled = enabled;
  return publicAugmentation(record);
}

async function activateAugmentation(record) {
  const viewer = record.manifest.spec.viewer || {};
  const render = record.manifest.spec.render || {};
  for (const script of render.scripts || []) await registerScript({ augmentationId: record.id, script, matches: record.matches, excludeMatches: record.excludeMatches });
  for (const style of render.styles || []) await insertStyle({ augmentationId: record.id, id: style.id, css: style.css });
  if ((viewer.networkRules || []).length > 0) await installNetworkRules({ augmentationId: record.id, rules: viewer.networkRules });
}

async function deactivateAugmentation(id) {
  for (const key of [...registrations.keys()]) if (key.startsWith(`${id}:`)) await unregisterScript({ augmentationId: id, id: key.slice(id.length + 1) });
  for (const key of [...styles.keys()]) if (key.startsWith(`${id}:`)) await removeStyle({ augmentationId: id, id: key.slice(id.length + 1) });
  const ownedRuleIds = [...networkRules.values()].filter((entry) => entry.augmentationId === id).map((entry) => entry.rule.id);
  if (ownedRuleIds.length > 0) await removeNetworkRules({ augmentationId: id, ruleIds: ownedRuleIds });
}

async function executeScript(input) {
  const source = requireString(input.source ?? input.script?.source, "source");
  const page = await targetPage(input.target);
  return { value: await page.evaluate((code) => (0, eval)(code), source) };
}

async function registerScript(input) {
  const augmentationId = requireString(input.augmentationId, "augmentationId");
  const script = input.script || input;
  const id = requireString(script.id, "script.id");
  const source = requireString(script.source, "script.source");
  const matches = Array.isArray(input.matches) ? input.matches : ["*://*/*"];
  const excludeMatches = Array.isArray(input.excludeMatches) ? input.excludeMatches : [];
  const key = `${augmentationId}:${id}`;
  if (registrations.has(key)) throw new Error(`script ${JSON.stringify(key)} already exists`);
  const handles = [];
  for (const page of (await getPages()).filter((item) => !isExtensionPage(item))) {
    handles.push({ page, handle: await addPreloadScript(page, scriptBootstrap, source, matches, excludeMatches) });
  }
  registrations.set(key, { source, matches, excludeMatches, handles });
  return { ok: true, id };
}

async function unregisterScript(input) {
  const key = `${requireString(input.augmentationId, "augmentationId")}:${requireString(input.id, "id")}`;
  const record = registrations.get(key);
  if (!record) throw new Error(`script ${JSON.stringify(key)} does not exist`);
  for (const { page, handle } of record.handles) await removePreloadScript(page, handle).catch(() => {});
  registrations.delete(key);
  return { ok: true };
}

async function insertStyle(input) {
  const key = `${requireString(input.augmentationId, "augmentationId")}:${requireString(input.id ?? "default", "id")}`;
  if (styles.has(key)) throw new Error(`style ${JSON.stringify(key)} already exists`);
  const css = requireString(input.css, "css");
  const handles = [];
  for (const page of (await getPages()).filter((item) => !isExtensionPage(item))) handles.push(await page.addStyleTag({ content: css }));
  styles.set(key, handles);
  return { ok: true };
}

async function removeStyle(input) {
  const key = `${requireString(input.augmentationId, "augmentationId")}:${requireString(input.id ?? "default", "id")}`;
  const handles = styles.get(key);
  if (!handles) throw new Error(`style ${JSON.stringify(key)} does not exist`);
  for (const handle of handles) await handle.evaluate((node) => node.remove()).catch(() => {});
  styles.delete(key);
  return { ok: true };
}

async function domQuery(input) {
  const selector = requireString(input.selector, "selector");
  const limit = Math.min(Math.max(Number(input.limit) || 25, 1), 100);
  return targetPage(input.target).then((page) => page.evaluate(({ selector, limit }) => ({
    matches: [...document.querySelectorAll(selector)].slice(0, limit).map((node) => ({ tag: node.tagName.toLowerCase(), id: node.id || null, text: (node.textContent || "").trim().slice(0, 500) })),
  }), { selector, limit }));
}

async function domObserve(input) {
  const selector = requireString(input.selector, "selector");
  const id = String(input.id || `observer-${Date.now()}`);
  const page = await targetPage(input.target);
  await ensureEventBinding(page);
  await page.evaluate(({ id, selector }) => {
    const root = globalThis.__jangolovaCymonkeyObservers ||= new Map();
    root.get(id)?.disconnect();
    const observer = new MutationObserver((mutations) => globalThis.__jangolovaCymonkeyEmit({ type: "document.mutation", data: { id, count: mutations.length } }));
    for (const node of document.querySelectorAll(selector)) observer.observe(node, { subtree: true, childList: true, attributes: true, characterData: true });
    root.set(id, observer);
  }, { id, selector });
  return { ok: true, id };
}

async function domPatch(input) {
  const selector = requireString(input.selector, "selector");
  const page = await targetPage(input.target);
  return page.evaluate((patch) => {
    const node = document.querySelector(patch.selector);
    if (!node) throw new Error(`selector ${JSON.stringify(patch.selector)} did not match`);
    if (typeof patch.text === "string") node.textContent = patch.text;
    if (patch.attributes && typeof patch.attributes === "object") for (const [name, value] of Object.entries(patch.attributes)) value === null ? node.removeAttribute(name) : node.setAttribute(name, String(value));
    if (patch.remove === true) node.remove();
    return { ok: true };
  }, input);
}

async function overlayChange(input, operation) {
  const id = requireString(input.id, "id");
  const page = await targetPage(input.target);
  return page.evaluate(({ input, operation }) => {
    const selector = `[data-jangolova-cymonkey-overlay="${CSS.escape(input.id)}"]`;
    let host = document.querySelector(selector);
    if (operation === "unmount") { if (!host) throw new Error("overlay does not exist"); host.remove(); return { ok: true }; }
    if (operation === "mount" && host) throw new Error("overlay already exists");
    if (operation === "patch" && !host) throw new Error("overlay does not exist");
    if (!host) { host = document.createElement("div"); host.dataset.jangolovaCymonkeyOverlay = input.id; host.attachShadow({ mode: "open" }); document.documentElement.append(host); }
    host.shadowRoot.innerHTML = `<style>${input.css || ""}</style><div>${input.html || ""}</div>`;
    return { ok: true, id: input.id };
  }, { input: { ...input, id }, operation });
}

function storageGet(input) {
  const augmentationId = requireString(input.augmentationId, "augmentationId");
  const values = Object.fromEntries((input.keys || []).map((key) => [key, storage.get(`${augmentationId}:${key}`) ?? null]));
  return { values };
}

function storageSet(input) {
  const augmentationId = requireString(input.augmentationId, "augmentationId");
  if (!input.values || typeof input.values !== "object") throw new Error("values must be an object");
  for (const [key, value] of Object.entries(input.values)) storage.set(`${augmentationId}:${key}`, value);
  return { ok: true, keys: Object.keys(input.values).sort() };
}

async function installNetworkRules(input) {
  if (targetProtocol !== "cdp") throw new Error("network.rules.install is unavailable on this probed backend");
  const augmentationId = requireString(input.augmentationId, "augmentationId");
  if (!Array.isArray(input.rules) || input.rules.length === 0) throw new Error("rules must be a non-empty array");
  for (const rule of input.rules) {
    if (!rule || !Number.isInteger(rule.id) || rule.id <= 0 || !rule.action || !rule.condition) throw new Error("each network rule requires a positive integer id, action, and condition");
    const existing = networkRules.get(rule.id);
    if (existing && existing.augmentationId !== augmentationId) throw new Error(`network rule ${rule.id} belongs to augmentation ${JSON.stringify(existing.augmentationId)}`);
  }
  for (const rule of input.rules) networkRules.set(rule.id, { augmentationId, rule });
  for (const page of (await getPages()).filter((item) => !isExtensionPage(item))) await enableInterception(page);
  return { ok: true, ruleIds: input.rules.map((rule) => rule.id).sort((a, b) => a - b) };
}

async function removeNetworkRules(input) {
  const augmentationId = requireString(input.augmentationId, "augmentationId");
  if (!Array.isArray(input.ruleIds) || input.ruleIds.length === 0) throw new Error("ruleIds must be a non-empty array");
  for (const id of input.ruleIds) {
    const existing = networkRules.get(id);
    if (!existing || existing.augmentationId !== augmentationId) throw new Error(`network rule ${id} is not owned by augmentation ${JSON.stringify(augmentationId)}`);
  }
  for (const id of input.ruleIds) networkRules.delete(id);
  if (networkRules.size === 0) await disableInterception();
  return { ok: true, ruleIds: [...input.ruleIds].sort((a, b) => a - b) };
}

async function enableInterception(page) {
  if (interceptionHandlers.has(page) || page.isClosed()) return;
  const handler = (request) => void handleInterceptedRequest(request);
  await page.setRequestInterception(true);
  page.on("request", handler);
  interceptionHandlers.set(page, handler);
}

async function disableInterception(selectedPages = null) {
  const selected = selectedPages ? new Set(selectedPages) : null;
  const pending = [];
  for (const [page, handler] of interceptionHandlers) {
    if (selected && !selected.has(page)) continue;
    page.off("request", handler);
    if (!page.isClosed()) pending.push(page.setRequestInterception(false).catch(() => {}));
    interceptionHandlers.delete(page);
  }
  await Promise.all(pending);
}

async function handleInterceptedRequest(request) {
  if (request.isInterceptResolutionHandled?.()) return;
  const matching = [...networkRules.values()]
    .map((entry) => entry.rule)
    .filter((rule) => networkRuleMatches(rule, request))
    .sort((left, right) => (Number(right.priority) || 1) - (Number(left.priority) || 1));
  const rule = matching[0];
  if (!rule) return request.continue().catch(() => {});
  const action = rule.action;
  if (action.type === "block") return request.abort("blockedbyclient").catch(() => {});
  if (action.type === "redirect" && typeof action.redirect?.url === "string") return request.continue({ url: action.redirect.url }).catch(() => {});
  if (action.type === "upgradeScheme") return request.continue({ url: request.url().replace(/^http:/, "https:") }).catch(() => {});
  if (action.type === "modifyHeaders") return request.continue({ headers: modifyRequestHeaders(request.headers(), action.requestHeaders) }).catch(() => {});
  return request.continue().catch(() => {});
}

function networkRuleMatches(rule, request) {
  const condition = rule.condition || {};
  const url = request.url();
  if (typeof condition.urlFilter === "string" && !url.includes(condition.urlFilter.replace(/^\|+|\|+$/g, ""))) return false;
  if (typeof condition.regexFilter === "string") {
    try { if (!new RegExp(condition.regexFilter).test(url)) return false; } catch { return false; }
  }
  if (Array.isArray(condition.resourceTypes) && !condition.resourceTypes.includes(request.resourceType())) return false;
  if (Array.isArray(condition.excludedResourceTypes) && condition.excludedResourceTypes.includes(request.resourceType())) return false;
  return true;
}

function modifyRequestHeaders(original, operations) {
  const headers = { ...original };
  for (const operation of Array.isArray(operations) ? operations : []) {
    const name = String(operation.header || "").toLowerCase();
    if (!name || /[\r\n\0]/.test(name + String(operation.value || ""))) continue;
    if (operation.operation === "remove") delete headers[name];
    else if (operation.operation === "append") headers[name] = `${headers[name] ? `${headers[name]}, ` : ""}${operation.value || ""}`;
    else if (operation.operation === "set") headers[name] = String(operation.value || "");
  }
  return headers;
}

async function ensureEventBinding(page) {
  await page.exposeFunction("__jangolovaCymonkeyEmit", (event) => appendEvent(String(event?.type || "page.event"), event?.data || {})).catch((error) => {
    if (!String(error?.message).includes("already exists")) throw error;
  });
}

async function targetPage(target) {
  const pages = (await getPages()).filter((page) => !isExtensionPage(page));
  const index = Number(target?.pageIndex);
  const page = Number.isInteger(index) ? pages[index] : pages[pages.length - 1];
  if (!page) throw new Error("Cymonkey has no target page");
  if (!originAllowed(page.url())) throw new Error(`Cymonkey policy denied target origin ${JSON.stringify(page.url())}`);
  return page;
}

async function enforceOriginPolicy(input) {
  for (const key of ["url", "origin"]) if (typeof input[key] === "string" && !originAllowed(input[key])) throw new Error(`Cymonkey policy denied origin ${JSON.stringify(input[key])}`);
  if (Array.isArray(input.matches)) for (const value of input.matches) if (!originPatternAllowed(value)) throw new Error(`Cymonkey policy denied match pattern ${JSON.stringify(value)}`);
}

function baseCapabilities(protocol) {
  const backend = protocol === "webdriver-bidi" ? "bidi" : "cdp";
  const values = [
    cap("augmentation.install", backend, "mapped", "browser-session", "session", "external", ["manifest"]),
    cap("augmentation.update", backend, "mapped", "browser-session", "session", "external", ["manifest"]),
    cap("augmentation.uninstall", backend, "mapped", "browser-session", "session", "external", ["augmentationId"]),
    cap("augmentation.enable", backend, "mapped", "browser-session", "session", "write", ["augmentationId"]),
    cap("augmentation.disable", backend, "mapped", "browser-session", "session", "write", ["augmentationId"]),
    cap("augmentation.list", backend, "mapped", "call", "ephemeral", "read", []),
    cap("augmentation.describe", backend, "mapped", "call", "ephemeral", "read", ["augmentationId"]),
    cap("window.navigate", backend, "native", "call", "ephemeral", "write", ["url"]),
    cap("window.click", backend, "native", "call", "ephemeral", "write", ["selector"]),
    cap("window.fill", backend, "native", "call", "ephemeral", "write", ["selector", "value"]),
    cap("window.press", backend, "native", "call", "ephemeral", "write", ["selector", "key"]),
    cap("window.evaluate", backend, "native", "call", "ephemeral", "external", ["expression"]),
    cap("window.screenshot", backend, "native", "call", "ephemeral", "read", []),
    cap("script.execute", backend, "native", "call", "ephemeral", "external", ["source"], "render"),
    cap("script.register", backend, "native", "browser-session", "session", "external", ["augmentationId", "script"], "render"),
    cap("script.unregister", backend, "native", "browser-session", "session", "external", ["augmentationId", "id"], "render"),
    cap("style.insert", backend, "mapped", "document", "ephemeral", "write", ["augmentationId", "css"], "render"),
    cap("style.remove", backend, "mapped", "document", "ephemeral", "write", ["augmentationId"], "render"),
    cap("document.query", backend, "mapped", "call", "ephemeral", "read", ["selector"], "render"),
    cap("document.observe", backend, "mapped", "document", "ephemeral", "read", ["selector"], "render"),
    cap("document.patch", backend, "mapped", "document", "ephemeral", "write", ["selector"], "render"),
    cap("overlay.mount", backend, "emulated", "document", "ephemeral", "write", ["id"], "render"),
    cap("overlay.patch", backend, "emulated", "document", "ephemeral", "write", ["id"], "render"),
    cap("overlay.unmount", backend, "emulated", "document", "ephemeral", "write", ["id"], "render"),
    cap("network.observe", backend, "native", "browser-session", "session", "read", []),
    cap("storage.get", backend, "emulated", "browser-session", "session", "read", ["augmentationId", "keys"]),
    cap("storage.set", backend, "emulated", "browser-session", "session", "write", ["augmentationId", "values"]),
  ];
  if (protocol === "cdp") {
    values.push(cap("network.rules.install", backend, "mapped", "browser-session", "session", "external", ["augmentationId", "rules"]));
    values.push(cap("network.rules.remove", backend, "mapped", "browser-session", "session", "external", ["augmentationId", "ruleIds"]));
  }
  return values;
}

async function probeBaseCapabilities() {
  const candidates = baseCapabilities(targetProtocol);
  const supported = new Set(["storage.get", "storage.set", "network.observe"]);
  const pages = (await getPages()).filter((item) => !isExtensionPage(item) && originAllowed(item.url()));
  const page = pages[pages.length - 1];
  if (!page) return candidates.filter((item) => supported.has(item.name));
  try {
    await page.evaluate(() => true);
    for (const name of ["script.execute", "document.query", "document.observe", "document.patch", "overlay.mount", "overlay.patch", "overlay.unmount", "window.navigate", "window.click", "window.fill", "window.press", "window.evaluate", "window.screenshot"]) supported.add(name);
  } catch {}
  if (targetProtocol === "cdp" && typeof page.setRequestInterception === "function") {
    supported.add("network.rules.install");
    supported.add("network.rules.remove");
  }
  try {
    const handle = await addPreloadScript(page, () => undefined);
    await removePreloadScript(page, handle);
    for (const name of ["script.register", "script.unregister", "augmentation.install", "augmentation.update", "augmentation.uninstall", "augmentation.enable", "augmentation.disable", "augmentation.list", "augmentation.describe"]) supported.add(name);
  } catch {}
  try {
    const style = await page.addStyleTag({ content: ":root{}" });
    await style.evaluate((node) => node.remove());
    supported.add("style.insert");
    supported.add("style.remove");
  } catch {}
  return candidates.filter((item) => supported.has(item.name));
}

function cap(name, driver, support, lifetime, persistence, effect, required, domain = "viewer") {
  return {
    name, domain, runtime: "browser-dom", driver, support,
    lifetime: lifetime === "browser-session" ? "attachment" : lifetime === "document" ? "surface" : lifetime,
    persistence, effect, inputSchema: { type: "object", required, additionalProperties: true },
  };
}

function normalizeExtensionCapability(value) {
  const { backend: _backend, ...capability } = value;
  return {
    ...capability,
    domain: capability.domain || "viewer",
    runtime: "browser-dom",
    driver: "webextension",
    support: capability.support || "native",
    lifetime: capability.lifetime || "installation",
    persistence: capability.persistence || "persistent",
  };
}

function normalizeExtension(value) {
  const mode = ["auto", "disabled", "required"].includes(value?.mode) ? value.mode : "auto";
  return { mode, id: typeof value?.id === "string" ? value.id : "" };
}

function normalizePolicy(value) {
  return {
    allowedCapabilities: Array.isArray(value?.allowedCapabilities) ? value.allowedCapabilities.filter((item) => typeof item === "string") : [],
    allowedOrigins: Array.isArray(value?.allowedOrigins) ? value.allowedOrigins.filter((item) => typeof item === "string") : [],
  };
}

function capabilityAllowed(name) { return policy.allowedCapabilities.length === 0 || policy.allowedCapabilities.includes(name); }
function originAllowed(value) {
  if (policy.allowedOrigins.length === 0 || !value || value === "about:blank") return true;
  try { const url = new URL(value); return policy.allowedOrigins.some((pattern) => matchOrigin(pattern, url)); } catch { return false; }
}
function originPatternAllowed(value) {
  if (policy.allowedOrigins.length === 0) return true;
  return policy.allowedOrigins.some((allowed) => value === allowed || value.startsWith(allowed.replace("*", "")));
}
function matchOrigin(pattern, url) {
  const match = /^(\*|https?):\/\/([^/]+)$/.exec(pattern);
  if (!match || (match[1] !== "*" && match[1] !== url.protocol.slice(0, -1))) return false;
  const host = match[2].replaceAll(".", "\\.").replaceAll("*", ".*");
  return new RegExp(`^${host}$`).test(url.host);
}

function validateManifest(value) {
  if (!value || value.apiVersion !== protocolVersion || value.kind !== "Augmentation") throw new Error("manifest must be a cymonkey/v1alpha1 Augmentation");
  requireString(value.metadata?.id, "manifest.metadata.id");
  requireString(value.metadata?.revision, "manifest.metadata.revision");
  const target = value.spec?.targets?.find((item) => item?.domain === "render" && item?.runtime === "browser-dom");
  if (!target || !Array.isArray(target.match?.urlPatterns) || target.match.urlPatterns.length === 0) throw new Error("manifest requires a render/browser-dom target with match.urlPatterns");
}
function requireAugmentation(id) { const value = augmentations.get(requireString(id, "augmentationId")); if (!value) throw new Error(`augmentation ${JSON.stringify(id)} is not installed`); return value; }
function publicAugmentation(value) { return { id: value.id, revision: value.revision, enabled: value.enabled, domains: ["render"] }; }
function browserTarget(manifest) { return manifest.spec.targets.find((item) => item?.domain === "render" && item?.runtime === "browser-dom"); }
function requireString(value, name) { if (typeof value !== "string" || !value) throw new Error(`${name} is required`); return value; }

function pageBridgeBootstrap(driver = "cdp") {
  if (globalThis.cymonkey !== undefined && (globalThis.cymonkey === null || !["object", "function"].includes(typeof globalThis.cymonkey))) return;
  const root = globalThis.cymonkey ||= {};
  if (root.jangolova) return;
  const overlays = new Map();
  let cursor = 0;
  const pageEvents = [];
  const emit = (type, data) => { cursor += 1; pageEvents.push({ id: String(cursor), type, occurredAt: new Date().toISOString(), domain: "render", runtime: "browser-dom", driver, data }); };
  const act = async (name, input = {}) => {
    if (name === "document.query") return { matches: [...document.querySelectorAll(String(input.selector || ""))].slice(0, 100).map((node) => ({ tag: node.tagName.toLowerCase(), id: node.id || null, text: (node.textContent || "").trim().slice(0, 500) })) };
    if (name === "document.patch") { const node = document.querySelector(String(input.selector || "")); if (!node) throw new Error("selector did not match"); if (typeof input.text === "string") node.textContent = input.text; return { ok: true }; }
    if (["overlay.mount", "overlay.patch"].includes(name)) { let host = overlays.get(input.id); if (!host) { host = document.createElement("div"); host.dataset.jangolovaCymonkeyOverlay = input.id; host.attachShadow({ mode: "open" }); document.documentElement.append(host); overlays.set(input.id, host); } host.shadowRoot.innerHTML = `<style>${input.css || ""}</style><div>${input.html || ""}</div>`; emit(name === "overlay.mount" ? "overlay.mounted" : "overlay.patched", { id: input.id }); return { ok: true }; }
    if (name === "overlay.unmount") { const host = overlays.get(input.id); if (!host) throw new Error("overlay does not exist"); host.remove(); overlays.delete(input.id); emit("overlay.unmounted", { id: input.id }); return { ok: true }; }
    throw new Error(`page-safe Cymonkey does not expose ${JSON.stringify(name)}`);
  };
  root.jangolova = Object.freeze({
    hello: async () => ({ protocolVersion, implementation: { name: "cymonkey-page" }, domains: ["render"], runtimes: ["browser-dom"], drivers: [driver] }),
    capabilities: async () => [
      cap("document.query", driver, "mapped", "call", "ephemeral", "read", ["selector"], "render"),
      cap("document.patch", driver, "mapped", "surface", "ephemeral", "write", ["selector"], "render"),
      ...["overlay.mount", "overlay.patch", "overlay.unmount"].map((name) => cap(name, driver, "emulated", "surface", "ephemeral", "write", ["id"], "render")),
    ],
    describe: async () => ({ revision: String(cursor), surfaces: [{ id: "document:main", domain: "render", runtime: "browser-dom", kind: "document", label: document.title, properties: { url: location.href, readyState: document.readyState, overlays: [...overlays.keys()] } }], augmentations: [] }),
    act,
    events: async (query = {}) => ({ events: pageEvents.filter((event) => Number(event.id) > Number(query.after || 0)), cursor: String(cursor) }),
  });
}

function scriptBootstrap(source, matches, excludeMatches) {
  const wildcard = (pattern) => new RegExp(`^${pattern.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replaceAll("*", ".*")}$`);
  const url = location.href;
  if (!matches.some((pattern) => wildcard(pattern).test(url)) || excludeMatches.some((pattern) => wildcard(pattern).test(url))) return;
  (0, eval)(source);
}

async function callExtension(method, params) {
  if (!extensionControl || extensionControl.isClosed()) throw new Error("Cymonkey WebExtension control plane is unavailable");
  return extensionControl.evaluate(({ method, params }) => globalThis.cymonkeyDispatch(method, params), { method, params });
}
function activeDrivers() { return [targetProtocol === "webdriver-bidi" ? "bidi" : "cdp", ...(extensionControl ? ["webextension"] : [])]; }
function isExtensionPage(page) { return page.url().startsWith("chrome-extension://") || page.url().startsWith("moz-extension://"); }
function redactURL(value) { try { const url = new URL(value); url.username = ""; url.password = ""; url.search = ""; url.hash = ""; return url.toString(); } catch { return ""; } }

function readEvents(query) {
  const after = Number.parseInt(query.after || "0", 10);
  const limit = Math.min(Math.max(Number(query.limit) || 100, 1), 256);
  const types = new Set(Array.isArray(query.types) ? query.types : []);
  return { events: events.filter((event) => Number(event.id) > after && (types.size === 0 || types.has(event.type))).slice(0, limit), cursor: String(sequence) };
}
function appendEvent(type, data) { sequence += 1; const domain = /^(document|overlay|script|style)\./.test(type) ? "render" : "viewer"; events.push({ id: String(sequence), type, occurredAt: new Date().toISOString(), domain, runtime: "browser-dom", driver: targetProtocol === "webdriver-bidi" ? "bidi" : "cdp", data }); if (events.length > 1024) events.splice(0, events.length - 1024); }

async function resolveCDPEndpoint(endpoint, headers) {
  if (endpoint.startsWith("ws://") || endpoint.startsWith("wss://")) return endpoint;
  const response = await fetch(new URL("/json/version", endpoint), { headers: safeHeaders(headers) });
  if (!response.ok) throw new Error(`CDP discovery returned HTTP ${response.status}`);
  const discovery = await response.json();
  if (typeof discovery.webSocketDebuggerUrl !== "string" || !discovery.webSocketDebuggerUrl) throw new Error("CDP discovery returned no webSocketDebuggerUrl");
  return discovery.webSocketDebuggerUrl;
}
function safeHeaders(value) { return !value || typeof value !== "object" || Array.isArray(value) ? {} : Object.fromEntries(Object.entries(value).filter(([name, item]) => typeof item === "string" && !/[\r\n\0]/.test(name + item))); }
async function disconnect() {
  await disableInterception();
  if (extensionControlCreated && extensionControl && !extensionControl.isClosed()) await extensionControl.close().catch(() => {});
  // Never call Playwright Browser.close() here: the target belongs to the
  // caller. Ending this worker releases its remote connection without asking
  // the browser to close its target, contexts, or pages.
  browser = null;
  disconnected = true;
  return { disconnected: true };
}
function isConnected() {
  if (!browser || disconnected) return false;
  return typeof browser.isConnected === "function" ? browser.isConnected() : Boolean(browser.connected);
}
function requireConnection() { if (!isConnected()) throw new Error("Cymonkey target is disconnected"); }
function respond(value) { process.stdout.write(`${JSON.stringify(value)}\n`); }
