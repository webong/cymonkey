#!/usr/bin/env node
import readline from "node:readline";
import process from "node:process";

const adapterIndex = process.argv.indexOf("--adapter");
const adapter = adapterIndex >= 0 ? process.argv[adapterIndex + 1] : "";
if (!new Set(["playwright", "puppeteer"]).has(adapter)) {
  console.error(`unsupported browser interaction adapter: ${adapter}`);
  process.exit(2);
}

let browser;
let disconnected = false;
let sequence = 0;
const events = [];
const capabilities = [
  capability("window.navigate", "Navigate the active window", "write", ["url"]),
  capability("window.click", "Click an element in the active window", "write", ["selector"]),
  capability("window.fill", "Fill an editable element in the active window", "write", ["selector", "value"]),
  capability("window.press", "Press a key in the active window", "write", ["selector", "key"]),
  capability("window.evaluate", "Evaluate a bounded expression in the active window", "external", ["expression"]),
  capability("window.screenshot", "Capture the active window as base64 PNG", "read", []),
];

const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
let chain = Promise.resolve();
lines.on("line", (line) => {
  chain = chain.then(() => handleLine(line)).catch((error) => {
    console.error(error?.stack || String(error));
  });
});

async function handleLine(line) {
  let request;
  try {
    request = JSON.parse(line);
    const result = await dispatch(request.method, request.params || {});
    respond({ id: request.id, result });
    if (request.method === "disconnect") {
      setImmediate(() => process.exit(0));
    }
  } catch (error) {
    respond({ id: request?.id || 0, error: error?.message || String(error) });
  }
}

async function dispatch(method, params) {
  if (method === "connect") return connect(params.endpoint, params.protocol, params.headers);
  if (method === "reconnect") return reconnect(params.endpoint, params.protocol, params.headers);
  if (method === "disconnect") return disconnect();
  if (method === "health") return { connected: isConnected() };
  requireConnection();
  if (method === "hello") {
    return {
      protocolVersion: "jangolova.bridge/v1alpha1",
      implementation: { name: adapter },
      features: ["browser", "caller-owned-target"],
    };
  }
  if (method === "capabilities") return capabilities;
  if (method === "describe") return describe();
  if (method === "act") return act(params);
  if (method === "events") return readEvents(params);
  throw new Error(`unsupported interaction method ${method}`);
}

async function connect(endpoint, protocol = "cdp", headers = {}) {
  if (typeof endpoint !== "string" || endpoint.length === 0) {
    throw new Error("caller-owned browser endpoint is required");
  }
  browser = await openBrowser(endpoint, protocol, headers);
  observeBrowser(browser);
  appendEvent("browser.connected", { adapter, protocol });
  return { capabilities: capabilities.map((item) => item.name) };
}

async function reconnect(endpoint, protocol = "cdp", headers = {}) {
  requireConnection();
  const previous = browser;
  const replacement = await openBrowser(endpoint, protocol, headers);
  browser = replacement;
  disconnected = false;
  observeBrowser(replacement);
  await disconnectBrowser(previous).catch(() => {});
  appendEvent("browser.connection.renewed", { adapter, protocol });
  return { reconnected: true };
}

async function openBrowser(endpoint, protocol, headers) {
  if (adapter === "playwright") {
    if (protocol !== "cdp") throw new Error("Playwright attachment currently requires CDP");
    const { chromium } = await import("playwright-core");
    return chromium.connectOverCDP(endpoint, { headers: safeHeaders(headers) });
  } else {
    const { default: puppeteer } = await import("puppeteer-core");
    const connectionEndpoint = protocol === "cdp" ? await resolveCDPEndpoint(endpoint, headers) : endpoint;
    const option = protocol === "webdriver-bidi"
      ? { browserWSEndpoint: connectionEndpoint, protocol: "webDriverBiDi", headers: safeHeaders(headers) }
      : { browserWSEndpoint: connectionEndpoint, protocol: "cdp", headers: safeHeaders(headers) };
    return puppeteer.connect(option);
  }
}

function observeBrowser(candidate) {
  candidate.on("disconnected", () => {
    if (browser !== candidate) return;
    disconnected = true;
    appendEvent("browser.disconnected", {});
  });
}

async function disconnectBrowser(candidate) {
  if (!candidate) return;
  if (adapter === "puppeteer" && candidate.disconnect) candidate.disconnect();
  if (adapter === "playwright" && candidate.close) await candidate.close();
}

async function resolveCDPEndpoint(endpoint, headers) {
  if (endpoint.startsWith("ws://") || endpoint.startsWith("wss://")) return endpoint;
  const discoveryURL = new URL("/json/version", endpoint);
  const response = await fetch(discoveryURL, { headers: safeHeaders(headers) });
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

async function disconnect() {
  await disconnectBrowser(browser);
  disconnected = true;
  return { disconnected: true };
}

async function describe() {
  const pages = await allPages();
  return {
    adapter,
    pages: await Promise.all(
      pages.map(async (page, index) => ({
        index,
        url: page.url(),
        title: await page.title(),
      })),
    ),
  };
}

async function act(request) {
  const name = request?.name;
  const input = request?.input || {};
  const page = await activePage();
  let result;
  if (name === "window.navigate") {
    requireString(input.url, "url");
    const response = await page.goto(input.url, { waitUntil: "domcontentloaded" });
    result = { url: page.url(), status: response?.status?.() ?? null };
  } else if (name === "window.click") {
    requireString(input.selector, "selector");
    await page.locator(input.selector).click();
    result = { clicked: true };
  } else if (name === "window.fill") {
    requireString(input.selector, "selector");
    requireString(input.value, "value");
    await page.locator(input.selector).fill(input.value);
    result = { filled: true };
  } else if (name === "window.press") {
    requireString(input.selector, "selector");
    requireString(input.key, "key");
    await page.locator(input.selector).press(input.key);
    result = { pressed: input.key };
  } else if (name === "window.evaluate") {
    requireString(input.expression, "expression");
    result = { value: await page.evaluate(input.expression) };
  } else if (name === "window.screenshot") {
    result = { pngBase64: await page.screenshot({ encoding: "base64", fullPage: Boolean(input.fullPage) }) };
  } else {
    throw new Error(`unsupported browser action ${name}`);
  }
  appendEvent("browser.action", { name });
  return result;
}

function readEvents(query) {
  const after = Number.parseInt(query.after || "0", 10);
  const limit = Math.min(Math.max(Number(query.limit) || 100, 1), 256);
  const types = new Set(Array.isArray(query.types) ? query.types : []);
  const selected = events
    .filter((event) => Number(event.id) > after && (types.size === 0 || types.has(event.type)))
    .slice(0, limit);
  return { events: selected, cursor: String(sequence) };
}

async function allPages() {
  if (adapter === "puppeteer") return browser.pages();
  return browser.contexts().flatMap((context) => context.pages());
}

async function activePage() {
  const pages = await allPages();
  if (pages.length > 0) return pages[pages.length - 1];
  if (adapter === "puppeteer") return browser.newPage();
  const contexts = browser.contexts();
  if (contexts.length === 0) throw new Error("connected browser has no context");
  return contexts[0].newPage();
}

function isConnected() {
  if (!browser || disconnected) return false;
  return adapter === "puppeteer" ? browser.connected : browser.isConnected();
}

function requireConnection() {
  if (!isConnected()) throw new Error("browser target is disconnected");
}

function capability(name, description, effect, required) {
  return {
    name,
    description,
    effect,
    inputSchema: {
      type: "object",
      required,
      additionalProperties: true,
    },
  };
}

function appendEvent(type, data) {
  sequence += 1;
  events.push({ id: String(sequence), type, occurredAt: new Date().toISOString(), data });
  if (events.length > 256) events.splice(0, events.length - 256);
}

function requireString(value, name) {
  if (typeof value !== "string" || value.length === 0) throw new Error(`${name} is required`);
}

function respond(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}
