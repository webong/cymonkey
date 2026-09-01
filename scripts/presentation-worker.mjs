#!/usr/bin/env node
import readline from "node:readline";
import process from "node:process";

let browser;
let page;
let cdpSession;
let disconnected = false;
let activePolicy = {};
let assetPolicy;
const supportedArtifactTransports = new Set(["http", "https", "target-file"]);
const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
let chain = Promise.resolve();
lines.on("line", (line) => { chain = chain.then(() => handleLine(line)).catch((error) => console.error(error?.stack || String(error))); });

async function handleLine(line) {
  let request;
  try {
    request = JSON.parse(line);
    const result = await dispatch(request.method, request.params || {});
    respond({ id: request.id, result });
    if (request.method === "disconnect") setImmediate(() => process.exit(0));
  } catch (error) { respond({ id: request?.id || 0, error: error?.message || String(error) }); }
}

async function dispatch(method, params) {
  if (method === "connect") return connect(params.endpoint, params.source, params.policy, params.headers);
  if (method === "reconnect") return reconnect(params.endpoint, params.headers);
  if (method === "disconnect") return disconnect();
  if (method === "health") return { connected: isConnected() && Boolean(page) };
  requireConnection();
  if (method === "hello") return page.evaluate(() => window.cymonkey?.jangolova?.hello?.() || null);
  if (method === "capabilities") return page.evaluate(() => window.cymonkey?.jangolova?.capabilities?.() || []);
  if (method === "describe") return page.evaluate(() => window.cymonkey?.jangolova?.describe?.() || null);
  if (method === "act") {
    if (params.name === "presentation.mount") {
      return withActionTimeout("presentation.mount", activePolicy.mountTimeoutMillis, () => mountArtifact(params.input || {}));
    }
    if (params.name === "presentation.capture") {
      return withActionTimeout("presentation.capture", activePolicy.captureTimeoutMillis, () => (
        page.screenshot({ encoding: "base64", fullPage: Boolean(params.input?.fullPage) })
          .then((pngBase64) => ({ pngBase64 }))
      ));
    }
    if (params.name === "presentation.execute") {
      return withActionTimeout("presentation.execute", activePolicy.executeTimeoutMillis, () => (
        page.evaluate((request) => window.cymonkey?.jangolova?.act?.(request.name, request.input || {}), params)
      ));
    }
    return page.evaluate((request) => window.cymonkey?.jangolova?.act?.(request.name, request.input || {}), params);
  }
  if (method === "events") return page.evaluate((query) => window.cymonkey?.jangolova?.events?.(query || {}), params);
  throw new Error(`unsupported interaction method ${method}`);
}

async function connect(endpoint, source, policy = {}, headers = {}) {
  if (typeof endpoint !== "string" || endpoint.length === 0) throw new Error("CDP endpoint is required");
  activePolicy = policy || {};
  browser = await openBrowser(endpoint, headers);
  observeBrowser(browser);
  const pages = await browser.pages();
  page = pages.at(-1) || await browser.newPage();
  cdpSession = await page.target().createCDPSession();
  assetPolicy = await installAssetPolicy(page, policy.allowedAssetOrigins, source || page.url());
  if (typeof source === "string" && source.trim()) await page.goto(source, { waitUntil: "domcontentloaded" });
  const ready = await page.evaluate(() => Boolean(window.cymonkey?.jangolova && typeof window.cymonkey.jangolova.act === "function"));
  if (!ready) throw new Error("active page does not expose window.cymonkey.jangolova presentation bridge");
  const capabilities = await page.evaluate(() => (window.cymonkey.jangolova.capabilities?.() || []).map((item) => item.name));
  return { capabilities };
}
async function reconnect(endpoint, headers = {}) {
  requireConnection();
  const previous = browser;
  const previousPageURL = page?.url?.() || "";
  const previousSession = cdpSession;
  const replacement = await openBrowser(endpoint, headers);
  let replacementPage;
  let replacementSession;
  let replacementAssetPolicy;
  try {
    const replacementPages = await replacement.pages();
    replacementPage = replacementPages.find((candidate) => candidate.url() === previousPageURL)
      || replacementPages.at(-1)
      || await replacement.newPage();
    replacementSession = await replacementPage.target().createCDPSession();
    replacementAssetPolicy = await installAssetPolicy(replacementPage, activePolicy.allowedAssetOrigins, previousPageURL || replacementPage.url());
  } catch (error) {
    if (replacement?.disconnect) replacement.disconnect();
    throw error;
  }
  browser = replacement;
  page = replacementPage;
  cdpSession = replacementSession;
  assetPolicy = replacementAssetPolicy;
  disconnected = false;
  observeBrowser(replacement);
  if (previousSession?.detach) await previousSession.detach().catch(() => {});
  if (previous?.disconnect) previous.disconnect();
  return { reconnected: true };
}
async function openBrowser(endpoint, headers) {
  const { default: puppeteer } = await import("puppeteer-core");
  const connectionHeaders = safeHeaders(headers);
  const connectionEndpoint = await resolveCDPEndpoint(endpoint, connectionHeaders);
  return puppeteer.connect({ browserWSEndpoint: connectionEndpoint, protocol: "cdp", headers: connectionHeaders });
}
function observeBrowser(candidate) {
  candidate.on("disconnected", () => { if (browser === candidate) disconnected = true; });
}
async function resolveCDPEndpoint(endpoint, headers) {
  if (endpoint.startsWith("ws://") || endpoint.startsWith("wss://")) return endpoint;
  const discoveryURL = new URL("/json/version", endpoint);
  const response = await fetch(discoveryURL, { headers });
  if (!response.ok) throw new Error(`CDP discovery returned HTTP ${response.status}`);
  const discovery = await response.json();
  if (typeof discovery.webSocketDebuggerUrl !== "string" || !discovery.webSocketDebuggerUrl) {
    throw new Error("CDP discovery returned no webSocketDebuggerUrl");
  }
  return discovery.webSocketDebuggerUrl;
}
function safeHeaders(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  return Object.fromEntries(Object.entries(value).filter(([name, item]) => (
    typeof name === "string" && typeof item === "string" && !/[\r\n\0]/.test(name + item)
  )));
}

async function disconnect() { if (cdpSession?.detach) await cdpSession.detach().catch(() => {}); if (browser?.disconnect) browser.disconnect(); disconnected = true; return { disconnected: true }; }
async function mountArtifact(input) {
  const currentStateRevision = await readStateRevision();
  if (input.expectedStateRevision === undefined || input.expectedStateRevision === null) throw new Error("presentation.mount expectedStateRevision is required");
  if (String(input.expectedStateRevision) !== currentStateRevision) {
    throw new Error(`presentation state revision conflict: expected ${input.expectedStateRevision}, current ${currentStateRevision}`);
  }
  const artifact = input.artifact;
  if (!artifact || artifact.apiVersion !== "interaction.presentation/v1alpha1") throw new Error("presentation.mount requires an interaction.presentation/v1alpha1 artifact");
  if (artifact.kind !== "web.entrypoint") throw new Error(`web presentation does not support artifact kind ${JSON.stringify(artifact.kind)}`);
  const allowedTransports = Array.isArray(activePolicy.allowedArtifactTransports) && activePolicy.allowedArtifactTransports.length > 0
    ? activePolicy.allowedArtifactTransports
    : ["http", "https"];
  const location = (artifact.locations || []).find((candidate) => (
    candidate && supportedArtifactTransports.has(candidate.transport) && allowedTransports.includes(candidate.transport) && locationMatchesTransport(candidate) && locationAllowedBySourcePolicy(candidate)
  ));
  if (!location) throw new Error("presentation artifact has no supported allowed location");

  const previousOrigin = assetPolicy?.selfOrigin() || "";
  assetPolicy?.setSelfOrigin(location.uri);
  try {
    await page.goto(location.uri, { waitUntil: "domcontentloaded" });
    const ready = await page.evaluate(() => Boolean(window.cymonkey?.jangolova && typeof window.cymonkey.jangolova.act === "function"));
    if (!ready) throw new Error("mounted artifact does not expose window.cymonkey.jangolova presentation bridge");
  } catch (error) {
    assetPolicy?.setSelfOrigin(previousOrigin);
    throw error;
  }
  return {
    ok: true,
    artifactId: artifact.artifactId,
    artifactRevision: artifact.revision,
    stateRevision: await readStateRevision(),
    location: { transport: location.transport },
  };
}
async function readStateRevision() {
  const description = await page.evaluate(() => window.cymonkey?.jangolova?.describe?.() || null);
  const revision = description?.stateRevision ?? description?.revision;
  return revision === undefined || revision === null ? "0" : String(revision);
}
function locationMatchesTransport(location) {
  try {
    const scheme = new URL(location.uri).protocol;
    return (location.transport === "target-file" && scheme === "file:") || scheme === `${location.transport}:`;
  } catch { return false; }
}
function locationAllowedBySourcePolicy(location) {
  if (location.transport === "target-file") return true;
  const allowedOrigins = Array.isArray(activePolicy.allowedSourceOrigins) ? activePolicy.allowedSourceOrigins : [];
  return allowedOrigins.length === 0 || allowedOrigins.includes(originOf(location.uri));
}
async function withActionTimeout(action, timeoutMillis, operation) {
  const timeout = Number(timeoutMillis) > 0 ? Number(timeoutMillis) : (action === "presentation.capture" ? 10000 : action === "presentation.mount" ? 15000 : 5000);
  let timer;
  let timedOut = false;
  const operationPromise = Promise.resolve().then(operation);
  const timeoutPromise = new Promise((_, reject) => {
    timer = setTimeout(() => {
      timedOut = true;
      if (action === "presentation.execute") terminateExecution().catch(() => {});
      if (action === "presentation.mount") stopLoading().catch(() => {});
      reject(new Error(`${action} timed out after ${timeout}ms`));
    }, timeout);
  });
  try {
    return await Promise.race([operationPromise, timeoutPromise]);
  } finally {
    clearTimeout(timer);
    if (timedOut) operationPromise.catch(() => {});
  }
}
async function terminateExecution() {
  if (cdpSession?.send) await cdpSession.send("Runtime.terminateExecution");
}
async function stopLoading() {
  if (cdpSession?.send) await cdpSession.send("Page.stopLoading");
}
async function installAssetPolicy(targetPage, configuredRules, source) {
  const rules = Array.isArray(configuredRules) && configuredRules.length > 0 ? configuredRules : ["self", "data:", "blob:"];
  let selfOrigin = originOf(source);
  await targetPage.setBypassServiceWorker(true);
  await targetPage.setCacheEnabled(false);
  targetPage.on("request", (request) => {
    if (isAssetAllowed(request.url(), rules, selfOrigin)) request.continue().catch(() => {});
    else request.abort("blockedbyclient").catch(() => {});
  });
  await targetPage.setRequestInterception(true);
  return {
    selfOrigin: () => selfOrigin,
    setSelfOrigin: (value) => { selfOrigin = originOf(value) || value; },
  };
}
function isAssetAllowed(value, rules, selfOrigin) {
  if (value.startsWith("about:") || value.startsWith("chrome:") || value.startsWith("devtools:")) return true;
  if (value.startsWith("data:")) return rules.includes("data:");
  if (value.startsWith("blob:")) return rules.includes("blob:");
  const origin = originOf(value);
  return (rules.includes("self") && origin && origin === selfOrigin) || rules.includes(origin);
}
function originOf(value) {
  try { return new URL(value).origin; } catch { return ""; }
}
function isConnected() { return Boolean(browser) && !disconnected && browser.connected; }
function requireConnection() { if (!isConnected() || !page) throw new Error("presentation target is disconnected"); }
function respond(value) { process.stdout.write(`${JSON.stringify(value)}\n`); }
