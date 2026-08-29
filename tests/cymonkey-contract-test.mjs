import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = new URL("../", import.meta.url);
const browserExtensionPackage = "pkg/browser-ext/package.json";
const browserExtensionConfig = "pkg/browser-ext/wxt.config.ts";

async function source(path) {
  return readFile(new URL(path, root), "utf8");
}

test("Cymonkey worker provides CDP, BiDi, and Playwright drivers with integrated browser automation", async () => {
  const worker = await source("scripts/cymonkey-worker.mjs");
  const syntax = spawnSync(process.execPath, ["--check", fileURLToPath(new URL("scripts/cymonkey-worker.mjs", root))], { encoding: "utf8" });
  assert.equal(syntax.status, 0, syntax.stderr);
  assert.match(worker, /puppeteer\.connect/);
  assert.match(worker, /playwright-core/);
  assert.match(worker, /webDriverBiDi/);
  assert.match(worker, /browser\.navigate/);
  assert.match(worker, /browser\.click/);
  assert.match(worker, /browser\.fill/);
  assert.match(worker, /browser\.press/);
  assert.match(worker, /browser\.evaluate/);
  assert.match(worker, /browser\.screenshot/);
	assert.match(worker, /jangolova\.cymonkey\/v1alpha2/);
	assert.doesNotMatch(worker, /jangolova\.cymonkey\/v1alpha1/);
  assert.match(worker, /extensionConfig\.mode === "required"/);
  assert.match(worker, /baseCapabilities\(targetProtocol\)/);
  assert.match(worker, /chrome-extension:\/\//);
  assert.match(worker, /cymonkeyDispatch/);
  assert.doesNotMatch(worker, /puppeteer\.launch/);
  assert.doesNotMatch(worker, /chromium\.launch/);
});

test("one reversible live client is shared by CDP and BiDi fixtures", async () => {
  const clientPath = new URL("tests/cymonkey-live-client.mjs", root);
  const client = await source("tests/cymonkey-live-client.mjs");
  const cdpFixture = await source("tests/docker/browser-interaction-smoke-test.sh");
  const bidiFixture = await source("tests/docker/firefox-bidi-smoke-test.sh");
  const syntax = spawnSync(process.execPath, ["--check", fileURLToPath(clientPath)], { encoding: "utf8" });
  assert.equal(syntax.status, 0, syntax.stderr);
  assert.match(client, /augmentation\.install/);
  assert.match(client, /augmentation\.uninstall/);
  assert.match(client, /finally/);
  assert.match(cdpFixture, /cymonkey-live-client\.mjs[\s\S]*--expect-backend cdp/);
  assert.match(bidiFixture, /cymonkey-live-client\.mjs[\s\S]*--expect-backend bidi/);
});

test("v1alpha2 defines runtime-agnostic computer, render, and player domains", async () => {
  const protocol = JSON.parse(await source("src/cymonkey/protocol/v1alpha2/protocol.schema.json"));
  const augmentation = JSON.parse(await source("src/cymonkey/protocol/v1alpha2/augmentation.schema.json"));
  assert.equal(protocol.$defs.hello.properties.protocolVersion.const, "jangolova.cymonkey/v1alpha2");
  assert.deepEqual(protocol.$defs.domain.enum, ["computer", "render", "player"]);
  for (const field of ["domain", "runtime", "driver"]) {
    assert.ok(protocol.$defs.capability.required.includes(field), `capability schema missing ${field}`);
  }
  assert.equal(protocol.$defs.driver.type, "string");
  assert.match("example-driver", new RegExp(protocol.$defs.driver.pattern));
  assert.equal(augmentation.properties.apiVersion.const, "jangolova.cymonkey/v1alpha2");
  assert.deepEqual(augmentation.$defs.target.properties.domain.enum, ["computer", "render", "player"]);
  assert.ok(augmentation.$defs.target.required.includes("runtime"));
	assert.deepEqual(protocol.$defs.action.dependentRequired, {domain: ["runtime"], runtime: ["domain"]});
  assert.doesNotMatch(JSON.stringify(augmentation), /applescript\.execute|raw-apple-event/);
});

test("transport mappings expose semantics without raw protocol passthrough", async () => {
  const worker = await source("scripts/cymonkey-worker.mjs");
  const safari = await source("integrations/jangolova/cymonkey/safari_backend.go");
  for (const capability of ["augmentation.install", "script.execute", "script.register", "dom.query", "dom.observe", "dom.patch", "network.observe", "storage.get"]) {
    assert.match(worker, new RegExp(capability.replaceAll(".", "\\.")), `worker missing ${capability}`);
  }
  assert.match(safari, /browser\.evaluate/);
  assert.match(safari, /strings\.Contains\(lower, "preload"\)/);
  assert.match(safari, /network\.observe/);
  assert.doesNotMatch(worker, /cdp\.call|bidi\.call|browser\.api|chrome\.evaluate/);
});

test("CDP interception rules are augmentation-owned and cleaned up before disconnect", async () => {
  const worker = await source("scripts/cymonkey-worker.mjs");
  assert.match(worker, /existing\.augmentationId !== augmentationId/);
  assert.match(worker, /network rule \$\{id\} is not owned by augmentation/);
  assert.match(worker, /await disableInterception\(\).*browser\.disconnect\(\)/s);
  assert.match(worker, /page\.setRequestInterception\(false\)/);
  assert.match(worker, /protocol === "cdp"/);
});

test("WXT package builds once per browser with embedded Xallet Spook support", async () => {
  const pkg = JSON.parse(await source(browserExtensionPackage));
  const config = await source(browserExtensionConfig);
  assert.equal(pkg.name, "@jangolova/browser-extension");
  assert.match(pkg.scripts.build, /chrome.*edge.*firefox/);
  assert.doesNotMatch(JSON.stringify(pkg.scripts), /mode spoke|build:spoke|dev:spoke/);
  assert.match(config, /outDirTemplate: '\{\{browser\}\}-mv\{\{manifestVersion\}\}'/);
  assert.match(config, /'management'/);
  assert.match(config, /externally_connectable: browser === 'safari' \? undefined : \{ ids: \['\*'\] \}/);
  assert.match(config, /data_collection_permissions/);
});

test("Cymonkey separates its page-safe and privileged extension planes", async () => {
  const background = await source("pkg/browser-ext/entrypoints/background.ts");
  const content = await source("pkg/browser-ext/entrypoints/cymonkey.content.ts");
  const page = await source("pkg/browser-ext/entrypoints/cymonkey-main.ts");
  const capabilities = await source("pkg/browser-ext/src/capabilities.ts");
  const engine = await source("pkg/browser-ext/src/engine.ts");

  assert.match(page, /root\.cymonkey = Object\.freeze/);
  assert.match(content, /allowedPageActions/);
  assert.match(content, /cannot invoke privileged action/);
  assert.match(background, /acceptsExternalSender\(sender\.id\)/);
  assert.match(engine, /jangolova\.cymonkey\/v1alpha2/);
	assert.doesNotMatch(engine, /compatibleProtocols|v1alpha1/);
  assert.match(engine, /domains: \['computer'\]/);
  assert.match(engine, /runtimes: \['browser-dom'\]/);
  assert.match(engine, /jangolova-browser-extension-webextension/);

  for (const capability of [
    "script.execute", "script.register", "script.unregister", "style.insert", "style.remove",
    "network.rules.install", "network.rules.remove", "storage.get", "storage.set",
  ]) {
    assert.match(capabilities, new RegExp(capability.replaceAll(".", "\\.")), `missing ${capability}`);
    assert.doesNotMatch(page, new RegExp(capability.replaceAll(".", "\\.")), `page bridge exposes ${capability}`);
  }
  assert.doesNotMatch(background, /browser\.api|chrome\.evaluate/);
});

test("Xallet Spook registration activates at runtime and authenticates the discovered hub ID", async () => {
  const spook = await source("pkg/browser-ext/src/xallet-spook.ts");
  const background = await source("pkg/browser-ext/entrypoints/background.ts");
  const policy = await source("pkg/browser-ext/src/services/policy.ts");
  assert.match(spook, /extension\.name === hubName && extension\.enabled/);
  assert.match(spook, /REGISTER_SPOKE/);
  assert.match(spook, /UPDATE_SPOKE_STATE/);
  assert.match(spook, /senderId === this\.hubId/);
  assert.match(background, /new XalletSpookClient/);
  assert.doesNotMatch(background, /import\.meta\.env\.MODE/);
  assert.match(policy, /JANGOLOVA_EXTENSION_CALL/);
});
